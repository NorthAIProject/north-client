package day_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/day"
	dayd "github.com/NorthAIProject/north-client/internal/day/day"
	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/hydration"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/sleep"
	"github.com/NorthAIProject/north-client/internal/users"
)

func seedUser(t *testing.T, pool *pgxpool.Pool) users.User {
	t.Helper()
	u, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        "day@example.com",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Test",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestLoadComposesTheDay(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool)

	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	// Pin the clock to late morning so the kitchen rule is still ahead and
	// the night's blocks are behind.
	clock := today.Add(11 * time.Hour)

	healthSvc := health.NewService(health.NewRepository(pool))
	biometricSvc := biometrics.NewService(biometrics.NewRepository(pool))
	hydrationSvc := hydration.NewService(hydration.NewRepository(pool))
	rules := day.NewRepository(pool)

	svc := day.NewService(day.Options{
		Rules:      rules,
		Hydration:  hydrationSvc,
		Sleep:      sleep.NewService(sleep.NewRepository(pool)),
		Biometrics: biometricSvc,
		Health:     healthSvc,
		Activity:   activity.NewService(activity.NewRepository(pool), biometricSvc),
		Now:        func() time.Time { return clock },
	})

	if _, err := hydrationSvc.Log(ctx, user, 500); err != nil {
		t.Fatal(err)
	}
	if _, err := biometricSvc.Record(ctx, user.ID, biometrics.Input{
		WeightKg: 83.9, HeightCm: 181, DateOfBirth: time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC), Sex: "male",
	}); err != nil {
		t.Fatal(err)
	}

	at := func(h, m int) time.Time { return today.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute) }
	end := func(t time.Time) *time.Time { return &t }
	readings := []health.Reading{
		// The night: 23:00 yesterday to 07:00, with one awake block.
		{Metric: dayd.MetricSleepCore, Value: 120, Unit: "min", StartedAt: at(-1, 0), EndedAt: end(at(1, 0))},
		{Metric: dayd.MetricSleepDeep, Value: 90, Unit: "min", StartedAt: at(1, 0), EndedAt: end(at(2, 30))},
		{Metric: dayd.MetricSleepAwake, Value: 30, Unit: "min", StartedAt: at(2, 30), EndedAt: end(at(3, 0))},
		{Metric: dayd.MetricSleepREM, Value: 240, Unit: "min", StartedAt: at(3, 0), EndedAt: end(at(7, 0))},
		// The day's aggregates.
		{Metric: "dietary_water", Value: 250, Unit: "ml", StartedAt: today, EndedAt: end(today.AddDate(0, 0, 1))},
		{Metric: "active_calories", Value: 320, Unit: "kcal", StartedAt: today, EndedAt: end(today.AddDate(0, 0, 1))},
		{Metric: "exercise_minutes", Value: 25, Unit: "min", StartedAt: today, EndedAt: end(today.AddDate(0, 0, 1))},
		{Metric: "stand_hours", Value: 6, Unit: "count", StartedAt: today, EndedAt: end(today.AddDate(0, 0, 1))},
		{Metric: "time_in_daylight", Value: 84, Unit: "min", StartedAt: today, EndedAt: end(today.AddDate(0, 0, 1))},
	}
	if _, err := healthSvc.Ingest(ctx, user.ID, "apple_health", readings); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.SetRule(ctx, user.ID, dayd.Rule{Kind: dayd.RuleKitchenCloses, At: "20:00", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetRule(ctx, user.ID, dayd.Rule{Kind: dayd.RuleCaffeineCutoff, At: "09:00", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetRule(ctx, user.ID, dayd.Rule{Kind: dayd.RuleScreensOff, At: "22:00", Enabled: false}); err != nil {
		t.Fatal(err)
	}

	snap, err := svc.Load(ctx, user, today)
	if err != nil {
		t.Fatal(err)
	}

	if !snap.IsToday {
		t.Error("today should be today")
	}
	if snap.Water.TotalML != 750 {
		t.Errorf("water = %d, want the app's 500 plus Health's 250", snap.Water.TotalML)
	}
	if snap.Sleep == nil || snap.Sleep.TotalMinutes != 450 {
		t.Fatalf("sleep = %+v, want 450 minutes asleep", snap.Sleep)
	}
	if snap.Sleep.StageMinutes[dayd.StageAwake] != 30 || snap.Sleep.Source != "apple_health" {
		t.Errorf("stages = %v from %q", snap.Sleep.StageMinutes, snap.Sleep.Source)
	}
	if snap.Activity.Move.Value != 320 || snap.Activity.Exercise.Value != 25 || snap.Activity.Stand.Value != 6 {
		t.Errorf("rings = %+v", snap.Activity)
	}
	if snap.DaylightMinutes == nil || *snap.DaylightMinutes != 84 {
		t.Errorf("daylight = %v", snap.DaylightMinutes)
	}
	if snap.Body.BMI == nil || snap.Body.Category() != dayd.BMIOverweight {
		t.Errorf("body = %+v", snap.Body)
	}
	if snap.EnergyPercent == nil {
		t.Error("a staged night today should produce an energy estimate")
	}

	if len(snap.Markers) != 2 {
		t.Fatalf("markers = %+v, want the two enabled rules", snap.Markers)
	}
	for _, m := range snap.Markers {
		switch m.Kind {
		case dayd.RuleCaffeineCutoff:
			if !m.Passed {
				t.Error("09:00 has passed at 11:00")
			}
		case dayd.RuleKitchenCloses:
			if m.Passed {
				t.Error("20:00 has not passed at 11:00")
			}
		}
	}

	yesterday, err := svc.Load(ctx, user, today.AddDate(0, 0, -1))
	if err != nil {
		t.Fatal(err)
	}
	if yesterday.IsToday || yesterday.Water.TotalML != 0 || yesterday.Sleep != nil || yesterday.EnergyPercent != nil {
		t.Errorf("yesterday leaked today's data: %+v", yesterday)
	}
}

func TestRulesValidateAndDelete(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool)
	svc := day.NewService(day.Options{Rules: day.NewRepository(pool)})

	if _, err := svc.SetRule(ctx, user.ID, dayd.Rule{Kind: "nap", At: "13:00"}); err == nil {
		t.Error("unknown kind accepted")
	}
	if _, err := svc.SetRule(ctx, user.ID, dayd.Rule{Kind: dayd.RuleLastDrink, At: "25:00"}); err == nil {
		t.Error("bad time accepted")
	}
	if _, err := svc.SetRule(ctx, user.ID, dayd.Rule{Kind: dayd.RuleLastDrink, At: "21:30", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	// Setting it again replaces rather than duplicates.
	if _, err := svc.SetRule(ctx, user.ID, dayd.Rule{Kind: dayd.RuleLastDrink, At: "21:00", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	rules, err := svc.Rules(ctx, user.ID)
	if err != nil || len(rules) != 1 || rules[0].At != "21:00" {
		t.Fatalf("rules = %+v, %v", rules, err)
	}
	if err := svc.DeleteRule(ctx, user.ID, dayd.RuleLastDrink); err != nil {
		t.Fatal(err)
	}
	if rules, _ := svc.Rules(ctx, user.ID); len(rules) != 0 {
		t.Errorf("rule survived delete: %+v", rules)
	}
}

func TestParseDateFallsBackToToday(t *testing.T) {
	loc := time.FixedZone("x", -5*3600)
	now := time.Date(2026, 9, 26, 3, 0, 0, 0, time.UTC) // 22:00 on the 25th in x
	got := day.ParseDate("garbage", loc, now)
	if got.Day() != 25 {
		t.Errorf("fallback = %v, want the 25th in the reader's zone", got)
	}
	if got := day.ParseDate("2026-01-02", loc, now); got.Month() != 1 || got.Day() != 2 || got.Location() != loc {
		t.Errorf("parse = %v", got)
	}
}
