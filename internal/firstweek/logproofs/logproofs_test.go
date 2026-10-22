package logproofs_test

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/exercises"
	"github.com/NorthAIProject/north-client/internal/firstweek"
	"github.com/NorthAIProject/north-client/internal/firstweek/logproofs"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/lifts"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/mind"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/sleep"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Each proof has to agree with how its slice dates a log: sleep and check-ins
// by local date, journal entries by timestamp. A proof that reads the wrong
// day never completes anything, and nobody notices — the week just looks
// ignored. So each is checked against the real slice, for today and for the
// day before (which must stay unproven).
func TestEachProofFindsItsOwnLogOnTheRightDay(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user, err := users.NewService(users.NewRepository(pool)).Register(ctx, users.Registration{
		Email:        "proofs@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Ana",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatal(err)
	}

	sleepSvc := sleep.NewService(sleep.NewRepository(pool))
	goalSvc := goals.NewService(goals.NewRepository(pool))
	checkinSvc := checkins.NewService(checkins.NewRepository(pool), goalSvc)
	mindSvc := mind.NewService(mind.NewRepository(pool), checkinSvc)
	proofs := logproofs.For(
		sleepSvc,
		activity.NewService(activity.NewRepository(pool), nil),
		lifts.NewService(lifts.NewRepository(pool), exercises.NewService(exercises.NewRepository(pool))),
		meals.NewFoodLogService(meals.NewRepository(pool)),
		mindSvc, checkinSvc,
	)
	for _, tmpl := range firstweek.Templates {
		if proofs[tmpl] == nil {
			t.Errorf("no proof for template %q", tmpl)
		}
	}

	// A week whose day 1 is today, in the person's zone.
	local := time.Now().In(user.Location())
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	p := firstweek.Programme{Timezone: user.Timezone, StartedOn: today.AddDate(0, 0, -1)}

	if _, err := sleepSvc.LogFor(ctx, user, sleep.LocalDate(user, time.Now()), sleep.Input{DurationMinutes: 420}); err != nil {
		t.Fatalf("log sleep: %v", err)
	}
	if _, err := mindSvc.Create(ctx, user.ID, mind.Input{Content: "Three lines, as promised."}); err != nil {
		t.Fatalf("journal: %v", err)
	}
	if _, err := checkinSvc.UpsertToday(ctx, user, checkins.Input{Mood: 4, Energy: 3}); err != nil {
		t.Fatalf("check in: %v", err)
	}

	for _, tmpl := range []string{firstweek.TemplateSleep, firstweek.TemplateJournal, firstweek.TemplateCustom} {
		got, err := proofs[tmpl].LoggedOn(ctx, user, p.DayRange(1))
		if err != nil || !got {
			t.Errorf("%s: today = %v, %v; want proven", tmpl, got, err)
		}
		got, err = proofs[tmpl].LoggedOn(ctx, user, p.DayRange(0))
		if err != nil || got {
			t.Errorf("%s: yesterday = %v, %v; want unproven", tmpl, got, err)
		}
	}
	// Nothing trained and nothing eaten: those stay open, without error.
	for _, tmpl := range []string{firstweek.TemplateTrain, firstweek.TemplateProtein} {
		if got, err := proofs[tmpl].LoggedOn(ctx, user, p.DayRange(1)); err != nil || got {
			t.Errorf("%s: today = %v, %v; want unproven", tmpl, got, err)
		}
	}
}
