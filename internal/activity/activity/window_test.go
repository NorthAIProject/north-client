package activity

import (
	"strings"
	"testing"
	"time"
)

// The training window is the only multi-day thing the coach reads about
// exercise, and every line of it ends up in a prompt as a factual claim. These
// defend the claims that are easy to get subtly wrong: what counts as a rest
// day, and what a comparison means when there is nothing to compare against.

// completed builds a finished session, which is the only kind the window counts.
func completed(code string, start time.Time, dur time.Duration, kcal float64) Session {
	end := start.Add(dur)
	return Session{
		ActivityCode:   code,
		Status:         StatusCompleted,
		StartedAt:      start,
		EndedAt:        &end,
		CaloriesBurned: &kcal,
	}
}

func TestSummaryReportsSessionsTimeAndCalories(t *testing.T) {
	t.Parallel()

	since := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	w := NewTrainingWindow([]Session{
		completed("running_8kmh", since.Add(8*time.Hour), 45*time.Minute, 400),
		completed("running_8kmh", since.AddDate(0, 0, 2).Add(8*time.Hour), 35*time.Minute, 320),
	}, since, 7, time.UTC)

	got := w.Summary()
	if len(got) != 2 {
		t.Fatalf("Summary() = %v, want a volume line and a recovery line", got)
	}
	if !strings.Contains(got[0], "2 sessions") {
		t.Errorf("volume line = %q, want the session count", got[0])
	}
	if !strings.Contains(got[0], "1h20m") {
		t.Errorf("volume line = %q, want the summed duration", got[0])
	}
	if !strings.Contains(got[0], "720 kcal") {
		t.Errorf("volume line = %q, want the summed burn", got[0])
	}
}

func TestSummaryOfAnEmptyWindowSaysNothingAtAll(t *testing.T) {
	t.Parallel()

	// A week with nothing in it is not the same as a week of zeroes. The coach
	// has its own empty-state label for this heading; inventing "0 sessions,
	// 7 rest days" would invite a lecture about a week that may simply have
	// been logged somewhere else.
	w := NewTrainingWindow(nil, time.Now(), 7, time.UTC)

	if got := w.Summary(); got != nil {
		t.Fatalf("Summary() = %v, want nothing", got)
	}
}

func TestSummaryOmitsTheDeltaWhenThereIsNoPriorWeek(t *testing.T) {
	t.Parallel()

	// The percentage would be a division by zero, and rounding it into "up
	// 100%" would describe a trend that does not exist yet.
	since := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	w := NewTrainingWindow([]Session{
		completed("cycling_moderate", since.Add(8*time.Hour), time.Hour, 500),
	}, since, 7, time.UTC)
	w.PriorCalories = 0

	recovery := w.Summary()[1]
	if !strings.Contains(recovery, "nothing logged the week before") {
		t.Errorf("recovery line = %q, want an explicit note that there is no comparison", recovery)
	}
	if strings.Contains(recovery, "%") {
		t.Errorf("recovery line = %q, want no percentage", recovery)
	}
}

func TestSummaryReportsTheDirectionOfTheLoadChange(t *testing.T) {
	t.Parallel()

	since := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	base := []Session{completed("cycling_moderate", since.Add(8*time.Hour), time.Hour, 1000)}

	up := NewTrainingWindow(base, since, 7, time.UTC)
	up.PriorCalories = 800
	if got := up.Summary()[1]; !strings.Contains(got, "up 25%") {
		t.Errorf("recovery line = %q, want a 25%% increase", got)
	}

	down := NewTrainingWindow(base, since, 7, time.UTC)
	down.PriorCalories = 2000
	if got := down.Summary()[1]; !strings.Contains(got, "down 50%") {
		t.Errorf("recovery line = %q, want a 50%% decrease", got)
	}

	flat := NewTrainingWindow(base, since, 7, time.UTC)
	flat.PriorCalories = 1020
	if got := flat.Summary()[1]; !strings.Contains(got, "about the same") {
		t.Errorf("recovery line = %q, want a two percent change reported as steady", got)
	}
}

func TestRestDaysCountCalendarDaysNotGapsBetweenSessions(t *testing.T) {
	t.Parallel()

	// Three sessions crammed into one day still leaves six days of the week
	// untrained, and a coach reading "no rest days" would be wrong about the
	// single most important thing on this line.
	since := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	day := since.Add(6 * time.Hour)

	w := NewTrainingWindow([]Session{
		completed("strength_training", day, time.Hour, 300),
		completed("running_8kmh", day.Add(4*time.Hour), 30*time.Minute, 250),
		completed("yoga", day.Add(8*time.Hour), 45*time.Minute, 120),
	}, since, 7, time.UTC)

	if w.RestDays != 6 {
		t.Fatalf("RestDays = %d, want 6", w.RestDays)
	}
}

func TestRestDaysUseTheUsersTimezoneNotUTC(t *testing.T) {
	t.Parallel()

	// A session finishing at 09:00 in Auckland is 21:00 the previous day in
	// UTC. Bucketing in the wrong zone moves it to a different calendar day,
	// which changes the rest-day count and can push it outside the window.
	auckland, err := time.LoadLocation("Pacific/Auckland")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	since := time.Date(2026, 8, 10, 0, 0, 0, 0, auckland)
	sessions := []Session{
		completed("running_8kmh", since.Add(9*time.Hour), 30*time.Minute, 300),
		completed("running_8kmh", since.AddDate(0, 0, 1).Add(9*time.Hour), 30*time.Minute, 300),
	}

	local := NewTrainingWindow(sessions, since, 7, auckland)
	if local.RestDays != 5 {
		t.Fatalf("RestDays in Auckland = %d, want 5", local.RestDays)
	}
}

func TestRestDaysCountEveryDayOfAWindowWithNoTraining(t *testing.T) {
	t.Parallel()

	since := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)

	// Sessions that all ended before the window opened: the caller filters by
	// window, but the count must not go negative or wrap if one slips through.
	w := NewTrainingWindow([]Session{
		completed("running_8kmh", since.AddDate(0, 0, -10), time.Hour, 500),
	}, since, 7, time.UTC)

	if w.RestDays != 7 {
		t.Fatalf("RestDays = %d, want 7", w.RestDays)
	}
}

func TestOpenSessionsAreNotCounted(t *testing.T) {
	t.Parallel()

	// A run in progress has no final duration or burn. Counting it would make
	// the same week read differently depending on when the coach was asked.
	since := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	w := NewTrainingWindow([]Session{
		{ActivityCode: "running_8kmh", Status: StatusActive, StartedAt: since.Add(time.Hour)},
	}, since, 7, time.UTC)

	if w.Sessions != 0 {
		t.Fatalf("Sessions = %d, want 0", w.Sessions)
	}
	if got := w.Summary(); got != nil {
		t.Fatalf("Summary() = %v, want nothing", got)
	}
}

func TestSummaryNamesTheBusiestSportsWithoutTheirQualifiers(t *testing.T) {
	t.Parallel()

	// "Running (8 km/h)" is a precise thing to log and a clumsy thing to read
	// back in a sentence, and more than two labels reads as a list rather than
	// a characterisation of the week.
	since := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	w := NewTrainingWindow([]Session{
		completed("running_8kmh", since.Add(time.Hour), time.Hour, 500),
		completed("running_8kmh", since.AddDate(0, 0, 1).Add(time.Hour), time.Hour, 500),
		completed("running_8kmh", since.AddDate(0, 0, 2).Add(time.Hour), time.Hour, 500),
		completed("strength_training", since.AddDate(0, 0, 3).Add(time.Hour), time.Hour, 300),
		completed("strength_training", since.AddDate(0, 0, 4).Add(time.Hour), time.Hour, 300),
		completed("yoga", since.AddDate(0, 0, 5).Add(time.Hour), time.Hour, 100),
	}, since, 7, time.UTC)

	if len(w.TopSports) != 2 {
		t.Fatalf("TopSports = %v, want two", w.TopSports)
	}
	if w.TopSports[0] != "Running" || w.TopSports[1] != "Strength training" {
		t.Fatalf("TopSports = %v, want the two busiest without qualifiers", w.TopSports)
	}
	if got := w.Summary()[0]; !strings.Contains(got, "mostly Running and Strength training") {
		t.Errorf("volume line = %q, want the busiest sports named", got)
	}
}

func TestAnUnknownActivityCodeStillGetsNamed(t *testing.T) {
	t.Parallel()

	// A code that predates or postdates the MET table should degrade to itself
	// rather than leaving the sentence trailing off after a dash.
	since := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	w := NewTrainingWindow([]Session{
		completed("underwater_basket_weaving", since.Add(time.Hour), time.Hour, 200),
	}, since, 7, time.UTC)

	if got := w.Summary()[0]; !strings.Contains(got, "underwater_basket_weaving") {
		t.Errorf("volume line = %q, want the raw code as a fallback", got)
	}
}

func TestSummaryOmitsTheRouteLineWithoutAProvider(t *testing.T) {
	t.Parallel()

	since := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	w := NewTrainingWindow([]Session{
		completed("running_8kmh", since.Add(time.Hour), time.Hour, 500),
	}, since, 7, time.UTC)

	if got := w.Summary(); len(got) != 2 {
		t.Fatalf("Summary() = %v, want no route line for a manual logger", got)
	}
}

func TestSummaryReportsDistanceAndClimbWhenRecorded(t *testing.T) {
	t.Parallel()

	since := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	w := NewTrainingWindow([]Session{
		completed("running_8kmh", since.Add(time.Hour), time.Hour, 500),
	}, since, 7, time.UTC)
	w.Route = &RouteTotals{Activities: 3, DistanceM: 42150, ElevationM: 680}

	route := w.Summary()[2]
	if !strings.Contains(route, "42.1 km") {
		t.Errorf("route line = %q, want kilometres", route)
	}
	if !strings.Contains(route, "680 m of climb") {
		t.Errorf("route line = %q, want the climb", route)
	}
}

func TestARouteWithNoClimbOmitsTheClimb(t *testing.T) {
	t.Parallel()

	// Flat rides are normal, and "with 0 m of climb" reads as a fault.
	since := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	w := NewTrainingWindow([]Session{
		completed("cycling_moderate", since.Add(time.Hour), time.Hour, 500),
	}, since, 7, time.UTC)
	w.Route = &RouteTotals{Activities: 1, DistanceM: 20000}

	route := w.Summary()[2]
	if strings.Contains(route, "climb") {
		t.Errorf("route line = %q, want no mention of climb", route)
	}
	if !strings.Contains(route, "20.0 km") {
		t.Errorf("route line = %q, want the distance", route)
	}
}

func TestDurationReadsAsAPersonWouldSayIt(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in   time.Duration
		want string
	}{
		{35 * time.Minute, "35m"},
		{4*time.Hour + 20*time.Minute, "4h20m"},
		{time.Hour + 5*time.Minute, "1h05m"},
		{90*time.Second + 4*time.Hour, "4h02m"},
	} {
		if got := formatDuration(tc.in); got != tc.want {
			t.Errorf("formatDuration(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestOneSessionIsNotPluralised(t *testing.T) {
	t.Parallel()

	since := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	w := NewTrainingWindow([]Session{
		completed("running_8kmh", since.Add(time.Hour), 35*time.Minute, 280),
	}, since, 7, time.UTC)

	if got := w.Summary()[0]; !strings.Contains(got, "1 session,") {
		t.Errorf("volume line = %q, want a singular session", got)
	}
	if got := w.Summary()[1]; !strings.Contains(got, "6 rest days") {
		t.Errorf("recovery line = %q, want plural rest days", got)
	}
}

func TestAWeekWithNoRestDaysSaysSo(t *testing.T) {
	t.Parallel()

	// "0 rest days" is the number a coach most needs to notice, and reads
	// worse than the words do.
	since := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	sessions := make([]Session, 0, 7)
	for i := range 7 {
		sessions = append(sessions, completed("running_8kmh", since.AddDate(0, 0, i).Add(time.Hour), time.Hour, 500))
	}

	w := NewTrainingWindow(sessions, since, 7, time.UTC)
	if w.RestDays != 0 {
		t.Fatalf("RestDays = %d, want 0", w.RestDays)
	}
	if got := w.Summary()[1]; !strings.HasPrefix(got, "no rest days") {
		t.Errorf("recovery line = %q, want it spelled out", got)
	}
}
