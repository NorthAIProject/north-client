package insights

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/stats/stat"
	"github.com/NorthAIProject/north-client/internal/users"
)

// datedHealth answers by metric and window, like the repository does.
type datedHealth map[string][]health.Stored

func (f datedHealth) Between(_ context.Context, _ uuid.UUID, metric string, since, until time.Time) ([]health.Stored, error) {
	var out []health.Stored
	for _, r := range f[metric] {
		if !r.StartedAt.Before(since) && r.StartedAt.Before(until) {
			out = append(out, r)
		}
	}
	return out, nil
}

// add records one reading per day for days days ending on last, the value
// given by the day's offset back from last (0 is last).
func (f datedHealth) add(metric string, last time.Time, days int, value func(back int) float64) {
	for back := 0; back < days; back++ {
		at := last.AddDate(0, 0, -back).Add(9 * time.Hour)
		f[metric] = append(f[metric], health.Stored{Metric: metric, Value: value(back), StartedAt: at})
	}
}

func TestHealthReadingsFoldToOnePerDay(t *testing.T) {
	t.Parallel()
	d := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	f := datedHealth{
		"steps": {
			{Value: 3000, StartedAt: d.Add(8 * time.Hour)},
			{Value: 4500, StartedAt: d.Add(19 * time.Hour)},
		},
		"body_mass": {
			{Value: 80.4, StartedAt: d.Add(7 * time.Hour)},
			{Value: 80.0, StartedAt: d.Add(7*time.Hour + 5*time.Minute)},
		},
	}
	svc := &Service{health: f}
	rg := timerange.Between(d, d.AddDate(0, 0, 1))
	for key, want := range map[string]float64{"steps": 7500, "weight": 80.2} {
		m, _ := lookupMetric(key)
		points, err := m.load(context.Background(), svc, users.User{}, rg)
		if err != nil {
			t.Fatal(err)
		}
		if len(points) != 1 || points[0].Value != want || !points[0].At.Equal(d) {
			t.Errorf("%s = %+v, want one %v on %v", key, points, want, d)
		}
	}
}

func TestMetricPlacesTheLatestDayAgainstTheUsual(t *testing.T) {
	t.Parallel()
	last := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	f := datedHealth{}
	f.add("steps", last, 29, func(back int) float64 {
		switch {
		case back == 0:
			return 20000
		case back%2 == 0:
			return 8000
		default:
			return 10000
		}
	})
	svc := &Service{health: f}
	data, err := svc.Metric(context.Background(), users.User{}, timerange.Between(last.AddDate(0, 0, -6), last.AddDate(0, 0, 1)), "steps")
	if err != nil {
		t.Fatal(err)
	}
	if data.Usual == nil {
		t.Fatal("no usual for 28 days of steps")
	}
	if data.Usual.State != stat.Above || data.Usual.Baseline.Days != 28 || data.Usual.Latest.Value != 20000 {
		t.Errorf("usual = %+v", *data.Usual)
	}

	view, err := buildMetricView(data)
	if err != nil {
		t.Fatal(err)
	}
	if view.Usual == nil || !view.Health || view.Usual.Text != "Above your usual: 20000 on Thu 8 Oct, usually 7982–10018." {
		t.Errorf("view usual = %+v health %v", view.Usual, view.Health)
	}
}

func TestMetricWithoutEnoughHistoryHasNoUsual(t *testing.T) {
	t.Parallel()
	last := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	f := datedHealth{}
	f.add("steps", last, 5, func(int) float64 { return 9000 })
	data, err := (&Service{health: f}).Metric(context.Background(), users.User{}, timerange.Between(last.AddDate(0, 0, -6), last.AddDate(0, 0, 1)), "steps")
	if err != nil {
		t.Fatal(err)
	}
	if data.Usual != nil {
		t.Errorf("five days made a usual: %+v", *data.Usual)
	}
}

func TestHealthListsWhatThePersonTracksInCatalogOrder(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 8, 20, 0, 0, 0, time.UTC)
	today := timerange.StartOfDay(now)
	f := datedHealth{}
	f.add("resting_heart_rate", today, 30, func(back int) float64 { return 52 + float64(back%3) })
	f.add("steps", today.AddDate(0, 0, -1), 3, func(int) float64 { return 9000 })
	// VO2 max last read a month ago: no longer tracked, so not listed.
	f.add("vo2max", today.AddDate(0, 0, -30), 5, func(int) float64 { return 44 })

	rows, err := (&Service{health: f}).Health(context.Background(), users.User{Timezone: "UTC"}, now)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, r := range rows {
		keys = append(keys, r.Metric.Key)
	}
	if strings.Join(keys, ",") != "steps,resting-heart-rate" {
		t.Fatalf("listed %v", keys)
	}
	if rows[0].Usual != nil || len(rows[0].Recent) != 3 {
		t.Errorf("steps row = %+v", rows[0])
	}
	if rows[1].Usual == nil || len(rows[1].Recent) != healthRecentDays {
		t.Errorf("resting heart rate row recent %d usual %+v", len(rows[1].Recent), rows[1].Usual)
	}

	list := projectHealth(rows)
	if list.Metrics[0].Latest != "9000" || list.Metrics[0].Day != "2026-10-07" || list.Metrics[1].Usual == nil {
		t.Errorf("projected %+v", list)
	}
}

// The chip must agree with the range the sentence shows: 52 against a usual
// shown as 52–54 is within it, though 52 sits 1.2 deviations under 53.0.
func TestUsualStateFollowsTheNumbersAsShown(t *testing.T) {
	t.Parallel()
	m, _ := lookupMetric("resting-heart-rate")
	u := Usual{
		Latest:   point{At: time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC), Value: 52},
		Baseline: stat.Baseline{Mean: 53, SD: 0.83, Days: 28}, Z: -1.2, State: stat.Below,
	}
	v := usualView(m, u)
	if v.State != stat.Usual || v.Text != "Within your usual range: 52bpm on Fri 9 Oct, usually 52bpm–54bpm." {
		t.Errorf("view = %q %q", v.State, v.Text)
	}
	u.Latest.Value = 51
	if v := usualView(m, u); v.State != stat.Below {
		t.Errorf("51 against 52–54 = %q", v.State)
	}
}

// The types the phone started sending reach the health list on their own:
// a catalog entry is all a new metric needs.
func TestNewHealthTypesAppearInTheList(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 9, 20, 0, 0, 0, time.UTC)
	today := timerange.StartOfDay(now)
	f := datedHealth{}
	f.add("distance_walking_running", today, 3, func(int) float64 { return 2.5 })
	f.add("flights_climbed", today, 3, func(int) float64 { return 8 })
	f.add("mindful_minutes", today, 3, func(int) float64 { return 10 })
	f.add("walking_hr_avg", today, 3, func(int) float64 { return 98 })
	f.add("respiratory_rate", today, 3, func(int) float64 { return 14.5 })
	f.add("spo2", today, 3, func(int) float64 { return 97 })

	rows, err := (&Service{health: f}).Health(context.Background(), users.User{Timezone: "UTC"}, now)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range rows {
		got = append(got, r.Metric.Key+"="+formatMetric(r.Metric, r.Recent[len(r.Recent)-1].Value))
	}
	want := "walking-running-distance=2.5km,flights-climbed=8,mindful-minutes=10min,walking-heart-rate=98bpm,respiratory-rate=14.5/min,blood-oxygen=97%"
	if strings.Join(got, ",") != want {
		t.Errorf("health list =\n%s\nwant\n%s", strings.Join(got, ","), want)
	}
}
