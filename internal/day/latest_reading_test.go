package day_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/day"
	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

// The body card shows the newest reading in the 90 days up to a minute past
// now. Both ends of that window matter: nothing stops a device with a wrong
// clock from delivering a reading in the future, and it must not stand in for
// the person's weight.
func TestBodyCardShowsTheNewestReadingInTheWindow(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	healthSvc := health.NewService(health.NewRepository(pool))

	clock := time.Now().UTC().Truncate(time.Hour)
	since := clock.AddDate(0, 0, -90)
	until := clock.Add(time.Minute)
	type reading struct {
		metric string
		at     time.Time
		value  float64
	}
	weight := func(at time.Time, kg float64) reading { return reading{"body_mass", at, kg} }
	systolic := func(at time.Time, v float64) reading { return reading{"bp_systolic", at, v} }
	diastolic := func(at time.Time, v float64) reading { return reading{"bp_diastolic", at, v} }

	cases := []struct {
		name       string
		readings   []reading
		wantWeight float64 // 0 is none
		wantSys    int     // 0 is no blood pressure
		wantDia    int
		wantAt     time.Time
	}{
		{name: "nothing recorded"},
		{
			name:       "newest of several",
			readings:   []reading{weight(clock.Add(-72*time.Hour), 80), weight(clock.Add(-time.Hour), 81), weight(clock.Add(-24*time.Hour), 82)},
			wantWeight: 81,
		},
		{name: "older than the window", readings: []reading{weight(since.Add(-time.Second), 80)}},
		{name: "exactly at the start of the window", readings: []reading{weight(since, 80)}, wantWeight: 80},
		{
			name:       "a reading from the future does not count",
			readings:   []reading{weight(clock.Add(-time.Hour), 81), weight(clock.Add(2*time.Hour), 99)},
			wantWeight: 81,
		},
		{name: "only a reading from the future", readings: []reading{weight(clock.Add(2*time.Hour), 99)}},
		{name: "exactly at the end of the window", readings: []reading{weight(until, 99)}},
		{name: "just inside the end of the window", readings: []reading{weight(until.Add(-time.Second), 81)}, wantWeight: 81},
		{
			name: "blood pressure is each metric's newest, dated by the systolic",
			readings: []reading{
				systolic(clock.Add(-48*time.Hour), 110), diastolic(clock.Add(-48*time.Hour), 70),
				systolic(clock.Add(-2*time.Hour), 120.6), diastolic(clock.Add(-3*time.Hour), 80.4),
			},
			wantSys: 121, wantDia: 80, wantAt: clock.Add(-2 * time.Hour),
		},
		{name: "systolic without diastolic is no blood pressure", readings: []reading{systolic(clock.Add(-time.Hour), 120)}},
		{name: "diastolic without systolic is no blood pressure", readings: []reading{diastolic(clock.Add(-time.Hour), 80)}},
		{
			name:     "blood pressure half out of the window is no blood pressure",
			readings: []reading{systolic(clock.Add(-time.Hour), 120), diastolic(since.Add(-time.Hour), 80)},
		},
		{
			name:     "blood pressure from the future does not count",
			readings: []reading{systolic(clock.Add(2*time.Hour), 180), diastolic(clock.Add(2*time.Hour), 110), systolic(clock.Add(-time.Hour), 120), diastolic(clock.Add(-time.Hour), 80)},
			wantSys:  120, wantDia: 80, wantAt: clock.Add(-time.Hour),
		},
	}

	svc := day.NewService(day.Options{Rules: day.NewRepository(pool), Health: healthSvc, Now: func() time.Time { return clock }})
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			user := bodyCardUser(t, pool, fmt.Sprintf("body-%d@example.com", i))
			if len(c.readings) > 0 {
				in := make([]health.Reading, len(c.readings))
				for k, r := range c.readings {
					in[k] = health.Reading{Metric: r.metric, Value: r.value, Unit: "u", StartedAt: r.at}
				}
				if _, err := healthSvc.Ingest(ctx, user.ID, "apple_health", in); err != nil {
					t.Fatal(err)
				}
			}
			snap, err := svc.Load(ctx, user, clock)
			if err != nil {
				t.Fatal(err)
			}

			switch got := snap.Body.WeightKg; {
			case c.wantWeight == 0 && got != nil:
				t.Fatalf("weight = %v, want none", *got)
			case c.wantWeight != 0 && (got == nil || *got != c.wantWeight):
				t.Fatalf("weight = %v, want %v", got, c.wantWeight)
			}
			bp := snap.Body.BloodPressure
			if c.wantSys == 0 {
				if bp != nil {
					t.Fatalf("blood pressure = %+v, want none", *bp)
				}
				return
			}
			if bp == nil || bp.Systolic != c.wantSys || bp.Diastolic != c.wantDia || !bp.At.Equal(c.wantAt) {
				t.Fatalf("blood pressure = %+v, want %d/%d at %v", bp, c.wantSys, c.wantDia, c.wantAt)
			}
		})
	}
}

// A device reading only replaces the manual measurement when it is newer.
func TestBodyCardTakesTheNewerOfManualAndDeviceWeight(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	healthSvc := health.NewService(health.NewRepository(pool))
	biometricSvc := biometrics.NewService(biometrics.NewRepository(pool))

	// The clock is ahead of the wall clock so that a device reading can be
	// newer than a manual measurement, which is stamped with the wall clock.
	realNow := time.Now().UTC()
	clock := realNow.Add(48 * time.Hour)
	svc := day.NewService(day.Options{Rules: day.NewRepository(pool), Health: healthSvc, Biometrics: biometricSvc, Now: func() time.Time { return clock }})

	cases := []struct {
		name   string
		device time.Time
		want   float64
	}{
		{name: "device older than manual", device: realNow.Add(-24 * time.Hour), want: 83},
		{name: "device newer than manual", device: realNow.Add(24 * time.Hour), want: 79},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			user := bodyCardUser(t, pool, fmt.Sprintf("manual-%d@example.com", i))
			if _, err := biometricSvc.Record(ctx, user.ID, biometrics.Input{
				WeightKg: 83, HeightCm: 180, DateOfBirth: time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC), Sex: "male",
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := healthSvc.Ingest(ctx, user.ID, "apple_health", []health.Reading{
				{Metric: "body_mass", Value: 79, Unit: "kg", StartedAt: c.device},
			}); err != nil {
				t.Fatal(err)
			}
			snap, err := svc.Load(ctx, user, clock)
			if err != nil {
				t.Fatal(err)
			}
			if got := snap.Body.WeightKg; got == nil || *got != c.want {
				t.Fatalf("weight = %v, want %v", got, c.want)
			}
			if snap.Body.BMI == nil {
				t.Fatal("BMI missing with both weight and height known")
			}
		})
	}
}

func bodyCardUser(t *testing.T, pool *pgxpool.Pool, email string) users.User {
	t.Helper()
	u, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email: email, PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: "T", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	return u
}
