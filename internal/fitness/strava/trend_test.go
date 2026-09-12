package strava

import (
	"testing"
	"time"
)

// A Sunday, so "this week" is a full week and the current-week edge cases are
// deliberate rather than accidental.
var trendNow = time.Date(2026, 9, 13, 18, 0, 0, 0, time.UTC)

func ride(at time.Time, minutes int) Activity {
	return Activity{
		StravaID:    at.Unix(),
		Name:        "Session",
		SportType:   "Ride",
		StartDate:   at,
		MovingTimeS: minutes * 60,
		DistanceM:   float64(minutes) * 300,
	}
}

// build runs the calculation over a 12-week window ending on trendNow.
func build(t *testing.T, activities []Activity) Trend {
	t.Helper()
	weeks := 12
	last := time.Date(trendNow.Year(), trendNow.Month(), trendNow.Day(), 0, 0, 0, 0, time.UTC)
	first := last.AddDate(0, 0, -(weeks*7 - 1))
	since := first.AddDate(0, 0, -normalWindowDays)
	return buildTrend(activities, time.UTC, since, first, last, trendNow)
}

// The line is the whole point of the chart — it is what "normal" means — and
// its first drawn day has to already be an average. Starting the window at the
// same day as the chart would make the left-hand end climb out of nothing,
// which reads as somebody getting fitter when it is only the window filling up.
func TestTheNormalLineIsAlreadyAverageOnTheFirstDrawnDay(t *testing.T) {
	t.Parallel()

	// Sixty minutes every day, for far longer than the chart shows.
	var activities []Activity
	for i := 0; i < 200; i++ {
		activities = append(activities, ride(trendNow.AddDate(0, 0, -i), 60))
	}

	trend := build(t, activities)

	if len(trend.Normal) != len(trend.Days) {
		t.Fatalf("line has %d points for %d days", len(trend.Normal), len(trend.Days))
	}
	if got := trend.Normal[0]; got < 59 || got > 61 {
		t.Errorf("the line starts at %.1f min, want ~60 — the look-back did not reach back", got)
	}
}

func TestRestDaysAreDrawnNotOmitted(t *testing.T) {
	t.Parallel()

	trend := build(t, []Activity{ride(trendNow.AddDate(0, 0, -3), 45)})

	if len(trend.Days) != 12*7 {
		t.Fatalf("chart has %d days, want %d — the axis is not evenly spaced", len(trend.Days), 12*7)
	}
	var zero int
	for _, day := range trend.Days {
		if day.Minutes == 0 {
			zero++
		}
	}
	if zero != 12*7-1 {
		t.Errorf("%d empty days, want %d", zero, 12*7-1)
	}
}

// A day is not a bar somebody did one thing on. Summing two sessions into one
// number is exactly the loss that made the old scene unreadable.
func TestADayKeepsEverySessionSeparate(t *testing.T) {
	t.Parallel()

	day := trendNow.AddDate(0, 0, -2)
	morning := ride(day.Add(-10*time.Hour), 30)
	morning.Name = "Morning"
	evening := ride(day.Add(2*time.Hour), 45)
	evening.Name = "Evening"

	trend := build(t, []Activity{morning, evening})

	for _, d := range trend.Days {
		if d.Minutes == 0 {
			continue
		}
		if len(d.Sessions) != 2 {
			t.Fatalf("the day holds %d sessions, want 2", len(d.Sessions))
		}
		if d.Minutes != 75 {
			t.Errorf("the day totals %.0f minutes, want 75", d.Minutes)
		}
		return
	}
	t.Fatal("no day carried the sessions")
}

// The bands, and the two cases where refusing to answer is the answer.
func TestShapeAcrossTheBands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		recent, normal float64
		history        int
		want           TrendShape
	}{
		{"nothing recorded", 0, 0, 0, ShapeNoHistory},
		{"training, but no baseline yet", 300, 0, 10, ShapeTooNew},
		{"a fortnight is still too new", 300, 200, 14, ShapeTooNew},
		{"well down", 60, 300, 90, ShapeWellDown},
		{"easier week", 200, 300, 90, ShapeEasier},
		{"steady", 300, 300, 90, ShapeSteady},
		{"building", 400, 300, 90, ShapeBuilding},
		{"a big week", 500, 300, 90, ShapeBigWeek},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, got := shapeOf(tt.recent, tt.normal, tt.history); got != tt.want {
				t.Errorf("shape = %v, want %v", got, tt.want)
			}
		})
	}
}

// Somebody who has trained for three days does not get told they are having a
// big week. Inventing a trend out of one data point is the failure this whole
// change is a reaction to.
func TestAShortHistoryRefusesToJudge(t *testing.T) {
	t.Parallel()

	trend := build(t, []Activity{
		ride(trendNow.AddDate(0, 0, -1), 60),
		ride(trendNow.AddDate(0, 0, -2), 60),
	})

	if trend.Shape != ShapeTooNew {
		t.Errorf("shape = %v, want ShapeTooNew", trend.Shape)
	}
	if trend.Ratio != 0 {
		t.Errorf("ratio = %.2f, want 0 — there is nothing to compare against", trend.Ratio)
	}
}

func TestStreakBreaksOnAGapWeek(t *testing.T) {
	t.Parallel()

	// Three weeks back-to-back, then a gap, then more.
	var activities []Activity
	for _, weeksAgo := range []int{0, 1, 2, 4, 5} {
		activities = append(activities, ride(trendNow.AddDate(0, 0, -7*weeksAgo), 60))
	}

	if got := build(t, activities).StreakWeeks; got != 3 {
		t.Errorf("streak = %d, want 3 — the gap week did not end it", got)
	}
}

// It is Monday morning for somebody. A nine-week run has not ended because
// they have not been out yet today.
func TestTheCurrentWeekBeingEmptyDoesNotBreakTheStreak(t *testing.T) {
	t.Parallel()

	monday := time.Date(2026, 9, 14, 9, 0, 0, 0, time.UTC)
	last := time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, time.UTC)
	first := last.AddDate(0, 0, -(12*7 - 1))
	since := first.AddDate(0, 0, -normalWindowDays)

	var activities []Activity
	for _, weeksAgo := range []int{1, 2, 3} {
		activities = append(activities, ride(monday.AddDate(0, 0, -7*weeksAgo), 60))
	}

	trend := buildTrend(activities, time.UTC, since, first, last, monday)
	if trend.StreakWeeks != 3 {
		t.Errorf("streak = %d, want 3 — an unstarted week ended it", trend.StreakWeeks)
	}
}

// A day that has not happened is not a rest day, and counting it as one would
// drag the line down every time the page is opened before Sunday, then let it
// rise again with no training behind the change.
func TestUnarrivedDaysAreNotCountedAsRest(t *testing.T) {
	t.Parallel()

	// Wednesday: the chart's last day, with four days of the week still to come.
	wednesday := time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	last := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC) // the Sunday
	first := last.AddDate(0, 0, -(12*7 - 1))
	since := first.AddDate(0, 0, -normalWindowDays)

	var activities []Activity
	for i := 0; i < 120; i++ {
		activities = append(activities, ride(wednesday.AddDate(0, 0, -i), 60))
	}

	trend := buildTrend(activities, time.UTC, since, first, last, wednesday)

	var future int
	for _, day := range trend.Days {
		if day.Future {
			future++
		}
	}
	if future != 4 {
		t.Fatalf("%d days marked future, want 4", future)
	}
	if trend.CountedDays != len(trend.Days)-4 {
		t.Errorf("counted %d of %d days, want the four unarrived ones left out", trend.CountedDays, len(trend.Days))
	}

	// Sixty minutes every day that has happened: the line must still read 60,
	// not 60 diluted by four zeroes.
	end := trend.Normal[len(trend.Normal)-1]
	if end < 59 || end > 61 {
		t.Errorf("the line ends at %.1f min, want ~60 — unarrived days were averaged in", end)
	}
}
