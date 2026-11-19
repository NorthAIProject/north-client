package horizon

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/horizon/forecast"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

func registered(t *testing.T, repo *users.PostgresRepository) users.User {
	t.Helper()
	u, err := users.NewService(repo).Register(context.Background(), users.Registration{
		Email: "horizon@north.test", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: "Ana", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestSnapshotsAreRecordedOnceAWeekAndNeverRewritten(t *testing.T) {
	pool := testdb.New(t)
	u := registered(t, users.NewRepository(pool))
	ctx := context.Background()
	repo := NewRepository(pool)
	p := regular()
	svc := NewService(Options{Lifts: p, Plans: p, Readings: p, Recovery: p, Targets: p, Measurements: typed{p.typed}, Checks: checks{p.checks}, Snapshots: repo})

	wrote, err := svc.RecordSnapshots(ctx, u)
	if err != nil || !wrote {
		t.Fatalf("first run: %v %v", wrote, err)
	}
	four, err := repo.List(ctx, u.ID, 4, at.AddDate(0, 0, -1))
	if err != nil {
		t.Fatal(err)
	}
	// Two lifts, weight and waist, each at four weeks.
	if len(four) != 4 {
		t.Fatalf("%d four-week snapshots: %+v", len(four), four)
	}
	for _, s := range four {
		if !(s.Low <= s.Mid && s.Mid <= s.High) || !s.MadeOn.Equal(time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)) {
			t.Errorf("snapshot %+v", s)
		}
	}
	eight, _ := repo.List(ctx, u.ID, 8, at.AddDate(0, 0, -1))
	if len(eight) < 3 {
		t.Errorf("%d eight-week snapshots", len(eight))
	}

	// The same week again writes nothing.
	if wrote, err = svc.RecordSnapshots(ctx, u); err != nil || wrote {
		t.Errorf("second run: %v %v", wrote, err)
	}
	// Nor does a direct write for the same day change what was recorded.
	changed := four[0]
	changed.Low, changed.Mid, changed.High = 1, 2, 3
	if err = repo.Add(ctx, u.ID, []Snapshot{changed}); err != nil {
		t.Fatal(err)
	}
	again, _ := repo.List(ctx, u.ID, 4, at.AddDate(0, 0, -1))
	if again[0] != four[0] {
		t.Errorf("a recorded forecast was rewritten: %+v", again[0])
	}
}

// Stored forecasts that have come due replace the backtest, and are scored
// against what was then measured.
func TestRecordedForecastsReplaceTheBacktestOnceEnoughAreDue(t *testing.T) {
	pool := testdb.New(t)
	u := registered(t, users.NewRepository(pool))
	ctx := context.Background()
	repo := NewRepository(pool)
	p := regular()
	svc := NewService(Options{Lifts: p, Plans: p, Readings: p, Recovery: p, Targets: p, Measurements: typed{p.typed}, Checks: checks{p.checks}, Snapshots: repo})

	// Six weekly squat forecasts, all due by now. The squat's max was within
	// a kilo or two of 138 throughout: four ranges hold it, two do not.
	key := LiftSeries("barbell-squat")
	var snaps []Snapshot
	for i := 0; i < 6; i++ {
		day := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -28-7*i)
		s := Snapshot{MadeOn: day, SeriesKey: key, HorizonWeeks: 4, Low: 130, Mid: 138, High: 146}
		if i >= 4 {
			s.Low, s.Mid, s.High = 150, 155, 160
		}
		snaps = append(snaps, s)
	}
	// One not yet due, and one for a series nothing was measured for.
	snaps = append(snaps,
		Snapshot{MadeOn: time.Date(2026, 11, 11, 0, 0, 0, 0, time.UTC), SeriesKey: key, HorizonWeeks: 4, Low: 1, Mid: 2, High: 3},
		Snapshot{MadeOn: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), SeriesKey: LiftSeries("deadlift"), HorizonWeeks: 4, Low: 1, Mid: 2, High: 3},
	)
	if err := repo.Add(ctx, u.ID, snaps); err != nil {
		t.Fatal(err)
	}

	view, err := svc.View(ctx, u, forecast.Scenario{})
	if err != nil {
		t.Fatal(err)
	}
	squat := view.Lifts[0].TrackRecord
	if squat == nil || squat.Kind != RecordRecorded || squat.Hits != 4 || squat.Total != 6 || squat.Text != "Landed in range 4 of 6 times" {
		t.Errorf("squat record = %+v", squat)
	}
	if bench := view.Lifts[1].TrackRecord; bench == nil || bench.Kind != RecordBacktest {
		t.Errorf("bench record = %+v", bench)
	}
}
