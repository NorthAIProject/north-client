package stats_test

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
	"github.com/NorthAIProject/north-client/internal/stats"
	"github.com/NorthAIProject/north-client/internal/stats/stat"
)

func d(day int) time.Time { return time.Date(2026, 9, day, 0, 0, 0, 0, time.UTC) }

func tp(day, h, m int) *time.Time {
	t := time.Date(2026, 9, day, h, m, 0, 0, time.UTC)
	return &t
}

func TestSleepShape(t *testing.T) {
	t.Parallel()
	q := 4
	apitest.AssertGolden(t, "stats_sleep.golden.json", stats.ProjectSleep("week", stat.Sleep([]stat.Night{
		{Date: d(21), Minutes: 450, Start: tp(20, 23, 20), End: tp(21, 7, 10), Stages: map[string]int{"deep": 70, "rem": 95, "core": 285, "awake": 15}, Quality: &q},
		{Date: d(22), Minutes: 390},
	}, stats.SleepTargetMinutes)))
}

func TestCardioShape(t *testing.T) {
	t.Parallel()
	st := stats.CardioStats{
		CardioStats: stat.Cardio([]stat.Session{
			{Code: "running", Name: "Running", At: *tp(22, 7, 0), Seconds: 1800, DistanceM: 6000, Kcal: 420},
		}, d(21), d(28)),
		RestingHR: []stat.DayValue{{Day: d(21), Value: 52}, {Day: d(22), Value: 51}},
	}
	apitest.AssertGolden(t, "stats_cardio.golden.json", stats.ProjectCardio("week", st))
}

func TestCardioKindShape(t *testing.T) {
	t.Parallel()
	outdoor := false
	run := func(day, minutes int, km, hr float64) stat.Session {
		return stat.Session{
			Code: "running", Name: "Running", At: *tp(day, 7, 0), Seconds: minutes * 60,
			DistanceM: km * 1000, AvgHR: hr, ElevationM: 40, Indoor: &outdoor,
		}
	}
	k := stat.KindDetail("Running", []stat.Session{run(1, 30, 5, 158), run(8, 31, 5, 156), run(15, 58, 10, 152)})
	apitest.AssertGolden(t, "stats_cardio_kind.golden.json", stats.ProjectCardioKind(k))
}

func TestEatingShape(t *testing.T) {
	t.Parallel()
	apitest.AssertGolden(t, "stats_eating.golden.json", stats.ProjectEating("week", stat.Eating([]stat.FoodEntry{
		{At: *tp(21, 8, 0), Date: d(21), Label: "Oats", Kcal: 380, Protein: 13},
		{At: *tp(21, 21, 30), Date: d(21), Label: "Salmon", Kcal: 460, Protein: 40},
	}, 2200, 160, 80, 21)))
}

func TestPatternsShape(t *testing.T) {
	t.Parallel()
	apitest.AssertGolden(t, "stats_patterns.golden.json", stats.ProjectPatterns("quarter", 90, []stat.Finding{{
		Key: "sleep_late_caffeine", Title: "You sleep 42 min less after late caffeine",
		Detail: "6h 38m on 8 nights after caffeine past your cutoff, 7h 20m on 20 other nights.", Unit: "min",
		A: stat.Group{Label: "After caffeine past your cutoff", Mean: 398, N: 8},
		B: stat.Group{Label: "Other nights", Mean: 440, N: 20}, Diff: -42,
	}}))
}
