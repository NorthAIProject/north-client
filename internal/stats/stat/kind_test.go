package stat_test

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/stats/stat"
)

func run(month, d, hour int, km float64, minutes int, hr float64, indoor *bool) stat.Session {
	return stat.Session{
		Code: "running_9_8kmh", Name: "Running",
		At:      time.Date(2026, time.Month(month), d, hour, 0, 0, 0, time.UTC),
		Seconds: minutes * 60, DistanceM: km * 1000, AvgHR: hr, Indoor: indoor,
	}
}

func bestOf(k stat.KindStats, key string) (stat.Best, bool) {
	for _, b := range k.Bests {
		if b.Key == key {
			return b, true
		}
	}
	return stat.Best{}, false
}

func TestRunningKindTotalsPaceBestsAndHabits(t *testing.T) {
	t.Parallel()
	out, in := false, true
	sessions := []stat.Session{
		// August: two 5 km runs at 6:00/km, Tuesdays at 7.
		run(8, 4, 7, 5, 30, 160, &out),
		run(8, 11, 7, 5, 30, 160, &out),
		// September: faster, a 10 km at 5:30/km, and a treadmill 5 km.
		run(9, 1, 7, 10, 55, 155, &out),
		run(9, 8, 18, 5, 27, 150, &in),
	}
	k := stat.KindDetail("Running", sessions)

	if k.Measure != "pace" || k.Sessions != 4 || k.DistanceKm != 25 || k.Indoor != 1 || k.Outdoor != 3 {
		t.Errorf("totals = %+v", k)
	}
	// 142 minutes over 25 km.
	if k.AvgPace != 341 {
		t.Errorf("avg pace = %v s/km, want 341", k.AvgPace)
	}
	if len(k.Monthly) != 2 || k.Monthly[0].Value != 360 || k.Monthly[1].Value != 328 {
		t.Errorf("monthly pace = %+v, want 360 then 328", k.Monthly)
	}
	if b, ok := bestOf(k, "fastest_5k"); !ok || b.Value != 1620 || b.Unit != "s" {
		t.Errorf("fastest 5k = %+v", b)
	}
	if b, ok := bestOf(k, "fastest_10k"); !ok || b.Value != 3300 {
		t.Errorf("fastest 10k = %+v", b)
	}
	if b, ok := bestOf(k, "longest"); !ok || b.Value != 10 || b.Unit != "km" {
		t.Errorf("longest = %+v", b)
	}
	if k.UsualDay != "Tuesday" || k.UsualHour != 7 {
		t.Errorf("usual = %s at %d", k.UsualDay, k.UsualHour)
	}
	// Metres per heartbeat rises as the same heart moves faster: fitter.
	if len(k.Efficiency) != 2 || !(k.Efficiency[1].Value > k.Efficiency[0].Value) {
		t.Errorf("efficiency = %+v, want a rise", k.Efficiency)
	}
	if k.AvgHR != 156.3 {
		t.Errorf("avg HR = %v", k.AvgHR)
	}
}

func TestCyclingKindIsMeasuredInSpeedAndClimb(t *testing.T) {
	t.Parallel()
	ride := func(d int, km float64, minutes int, climb float64) stat.Session {
		return stat.Session{
			Code: "cycling_moderate", Name: "Cycling", At: time.Date(2026, 9, d, 9, 0, 0, 0, time.UTC),
			Seconds: minutes * 60, DistanceM: km * 1000, ElevationM: climb,
		}
	}
	k := stat.KindDetail("Cycling", []stat.Session{ride(5, 40, 90, 400), ride(12, 25, 60, 150)})
	if k.Measure != "speed" || k.AvgSpeed != 26 || k.ElevationM != 550 {
		t.Errorf("cycling = %+v", k)
	}
	if b, ok := bestOf(k, "most_climb"); !ok || b.Value != 400 || b.Unit != "m" {
		t.Errorf("most climb = %+v", b)
	}
	if b, ok := bestOf(k, "fastest_20k"); !ok || b.Value != 26.7 || b.Unit != "km/h" {
		t.Errorf("fastest 20k+ = %+v", b)
	}
	if len(k.Efficiency) != 0 {
		t.Errorf("efficiency without heart rate = %+v", k.Efficiency)
	}
	// Two sessions are too few to call a habit.
	if k.UsualDay != "" || k.UsualHour != -1 {
		t.Errorf("usual from two rides = %q %d", k.UsualDay, k.UsualHour)
	}
}

// A kind without distance, like a class or a rowing session logged by time,
// still gets totals and its longest session, and no pace.
func TestTimedKindHasLongestSessionOnly(t *testing.T) {
	t.Parallel()
	s := stat.Session{Code: "elliptical", Name: "Elliptical", At: time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC), Seconds: 2400}
	k := stat.KindDetail("Elliptical", []stat.Session{s})
	if k.AvgPace != 0 || len(k.Monthly) != 0 {
		t.Errorf("pace without distance: %+v", k)
	}
	if b, ok := bestOf(k, "longest_session"); !ok || b.Value != 40 || b.Unit != "min" || len(k.Bests) != 1 {
		t.Errorf("bests = %+v", k.Bests)
	}
}
