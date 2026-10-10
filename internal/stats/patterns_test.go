package stats_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/stats"
	"github.com/NorthAIProject/north-client/internal/users"
)

type fakeHealth map[string][]health.Stored

func (f fakeHealth) Between(_ context.Context, _ uuid.UUID, metric string, _, _ time.Time) ([]health.Stored, error) {
	return f[metric], nil
}

type fakeCheckIns []checkins.CheckIn

func (f fakeCheckIns) ListBetween(context.Context, uuid.UUID, timerange.Range) ([]checkins.CheckIn, error) {
	return f, nil
}

type fakeActivity []activity.Session

func (f fakeActivity) ListBetween(context.Context, uuid.UUID, timerange.Range) ([]activity.Session, error) {
	return f, nil
}

// Twelve days: on the even ones the person walked far, outside, in daylight,
// and checked in a point happier. Steps arrive as two readings a day, the way
// the ingest bridge sends them, so they must be summed rather than averaged.
func TestPatternsReadAppleHealthAndOutdoorSessions(t *testing.T) {
	t.Parallel()
	user := users.User{ID: uuid.New(), Timezone: "UTC"}
	today := timerange.StartOfDay(time.Now().UTC())

	hk := fakeHealth{}
	var checks fakeCheckIns
	var sessions fakeActivity
	for i := 1; i <= 12; i++ {
		d := today.AddDate(0, 0, -i)
		mood, steps, daylight := 3, 2000.0, 15.0
		if i%2 == 0 {
			mood, steps, daylight = 4, 5000, 80
			ended := d.Add(8 * time.Hour)
			sessions = append(sessions, activity.Session{ActivityCode: "walking_brisk", StartedAt: d.Add(7 * time.Hour), EndedAt: &ended})
		}
		hk["steps"] = append(hk["steps"],
			health.Stored{Metric: "steps", Value: steps, StartedAt: d.Add(9 * time.Hour)},
			health.Stored{Metric: "steps", Value: steps, StartedAt: d.Add(18 * time.Hour)})
		hk["time_in_daylight"] = append(hk["time_in_daylight"], health.Stored{Metric: "time_in_daylight", Value: daylight, StartedAt: d})
		checks = append(checks, checkins.CheckIn{LocalDate: d, Mood: mood, Energy: 3})
	}

	svc := stats.NewService(stats.Sources{Health: hk, CheckIns: checks, Activity: sessions})
	found, _, err := svc.Patterns(context.Background(), user, timerange.Parse(timerange.KeyMonth, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"mood_steps":    "10,000 steps or more",
		"mood_daylight": "1h 20m or more in daylight",
		"mood_outdoor":  "train outdoors",
	}
	for _, f := range found {
		if w, ok := want[f.Key]; ok {
			if f.Diff != 1 || !strings.Contains(f.Title, w) {
				t.Errorf("%s = %q diff %v, want %q", f.Key, f.Title, f.Diff, w)
			}
			delete(want, f.Key)
		}
	}
	for key := range want {
		t.Errorf("%s missing from %+v", key, found)
	}
}

// A treadmill run is not an outdoor workout once the provider says so, even
// though its activity code is a run.
func TestPatternsTrustTheIndoorFlag(t *testing.T) {
	t.Parallel()
	user := users.User{ID: uuid.New(), Timezone: "UTC"}
	today := timerange.StartOfDay(time.Now().UTC())
	indoor := true

	var checks fakeCheckIns
	var sessions fakeActivity
	for i := 1; i <= 12; i++ {
		d := today.AddDate(0, 0, -i)
		mood := 3
		if i%2 == 0 {
			mood = 4
			ended := d.Add(8 * time.Hour)
			sessions = append(sessions, activity.Session{
				ActivityCode: "running_fast", StartedAt: d.Add(7 * time.Hour), EndedAt: &ended, Indoor: &indoor,
			})
		}
		checks = append(checks, checkins.CheckIn{LocalDate: d, Mood: mood, Energy: 3})
	}

	svc := stats.NewService(stats.Sources{CheckIns: checks, Activity: sessions})
	found, _, err := svc.Patterns(context.Background(), user, timerange.Parse(timerange.KeyMonth, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range found {
		if f.Key == "mood_outdoor" {
			t.Errorf("treadmill runs counted as outdoor: %q", f.Title)
		}
	}
}

// One activity type's page reads only its own sessions: a treadmill run is
// "Indoor running", not "Running", and a gym session is not cardio.
func TestCardioKindKeepsToItsOwnSessions(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	indoor, outdoor := true, false
	at := func(days int) (time.Time, *time.Time) {
		start := now.AddDate(0, 0, -days)
		end := start.Add(30 * time.Minute)
		return start, &end
	}
	km, hr := 5000.0, 150.0
	var sessions fakeActivity
	for i, where := range []*bool{&outdoor, &outdoor, &indoor, nil} {
		start, end := at(i + 1)
		sessions = append(sessions, activity.Session{
			ActivityCode: "running_fast", StartedAt: start, EndedAt: end,
			DistanceM: &km, AvgHR: &hr, Indoor: where,
		})
	}
	start, end := at(5)
	sessions = append(sessions, activity.Session{ActivityCode: "strength_training", StartedAt: start, EndedAt: end})

	svc := stats.NewService(stats.Sources{Activity: sessions})
	user := users.User{ID: uuid.New(), Timezone: "UTC"}
	running, err := svc.CardioKind(context.Background(), user, "Running", now)
	if err != nil {
		t.Fatal(err)
	}
	// Two flagged outdoor and one that did not say.
	if running.Sessions != 3 || running.Outdoor != 2 || running.Indoor != 0 || running.AvgHR != 150 || running.AvgPace != 360 {
		t.Errorf("running = %+v", running)
	}
	treadmill, err := svc.CardioKind(context.Background(), user, "Indoor running", now)
	if err != nil || treadmill.Sessions != 1 || treadmill.Indoor != 1 {
		t.Errorf("indoor running = %+v, %v", treadmill, err)
	}
	if none, _ := svc.CardioKind(context.Background(), user, "Rowing", now); none.Sessions != 0 {
		t.Errorf("no rowing = %+v", none)
	}
}
