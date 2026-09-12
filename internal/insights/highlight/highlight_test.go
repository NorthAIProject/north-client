package highlight

import (
	"strings"
	"testing"
)

func week(values ...float64) Series {
	labels := []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}
	return Series{Label: "Sleep", Unit: "h", Values: values, Labels: labels[:len(values)]}
}

func TestFindNamesTheStandoutBucket(t *testing.T) {
	got := Find(Input{Series: []Series{week(6, 6, 6, 11, 6, 6, 6)}}, 3)

	if len(got) == 0 {
		t.Fatal("no highlights from a window with an obvious peak")
	}
	joined := strings.Join(got, " | ")
	if !strings.Contains(joined, "Thu") {
		t.Errorf("highlights do not name the peak day: %s", joined)
	}
}

func TestFindIgnoresAFlatWindow(t *testing.T) {
	// Nothing stood out. Saying "your best day was Tuesday" about seven
	// identical days is noise dressed as an insight.
	got := Find(Input{Series: []Series{week(7, 7, 7, 7, 7, 7, 7)}}, 3)

	for _, line := range got {
		if strings.Contains(line, "best") {
			t.Errorf("claimed a standout in a flat window: %q", line)
		}
	}
}

func TestFindReportsARunOfLoggedDays(t *testing.T) {
	got := Find(Input{Series: []Series{week(7, 7, 7, 7, 0, 0, 0)}}, 3)

	joined := strings.Join(got, " | ")
	if !strings.Contains(joined, "4") {
		t.Errorf("a four-day run is not reported: %s", joined)
	}
}

func TestFindReportsAGap(t *testing.T) {
	got := Find(Input{Series: []Series{week(7, 0, 0, 0, 0, 7, 7)}}, 3)

	joined := strings.Join(got, " | ")
	if !strings.Contains(strings.ToLower(joined), "unlogged") {
		t.Errorf("a four-day gap is not reported: %s", joined)
	}
}

func TestFindComparesWithThePriorWindow(t *testing.T) {
	got := Find(Input{
		Comparisons: []Comparison{{Label: "Burn", Current: 1200, Prior: 800}},
	}, 3)

	joined := strings.Join(got, " | ")
	if !strings.Contains(joined, "Burn") || !strings.Contains(joined, "50") {
		t.Errorf("a 50%% rise is not reported: %s", joined)
	}
}

func TestFindStaysSilentOnASmallChange(t *testing.T) {
	// A 3% move is noise. Reporting it teaches people to ignore the section.
	got := Find(Input{
		Comparisons: []Comparison{{Label: "Burn", Current: 1030, Prior: 1000}},
	}, 3)

	if len(got) != 0 {
		t.Errorf("reported a 3%% change: %v", got)
	}
}

func TestFindMakesNoClaimWithoutAPriorWindow(t *testing.T) {
	got := Find(Input{
		Comparisons: []Comparison{{Label: "Burn", Current: 1200, Prior: 0}},
	}, 3)

	if len(got) != 0 {
		t.Errorf("compared against a window that does not exist: %v", got)
	}
}

func TestFindHonoursTheLimit(t *testing.T) {
	got := Find(Input{
		Series: []Series{week(1, 1, 1, 9, 0, 0, 0), week(2, 2, 2, 14, 0, 0, 0)},
		Comparisons: []Comparison{
			{Label: "Burn", Current: 1200, Prior: 800},
			{Label: "Water", Current: 400, Prior: 900},
		},
	}, 2)

	if len(got) > 2 {
		t.Errorf("returned %d highlights, want at most 2", len(got))
	}
}

func TestFindOfNothingIsNothing(t *testing.T) {
	if got := Find(Input{}, 3); len(got) != 0 {
		t.Errorf("invented %d highlights from no data: %v", len(got), got)
	}
}

func TestFindNamesTheUnitOfTime(t *testing.T) {
	// "unlogged for 10 in a row" leaves the reader to guess ten of what. At
	// year grain the buckets are months, and the sentence has to say so.
	s := Series{Label: "Water", Period: "month", Values: []float64{1, 0, 0, 0, 0}, Labels: []string{"a", "b", "c", "d", "e"}}

	got := Find(Input{Series: []Series{s}}, 3)

	joined := strings.Join(got, " | ")
	if !strings.Contains(joined, "month") {
		t.Errorf("sentence does not name the period: %s", joined)
	}
}

func TestFindPluralisesThePeriod(t *testing.T) {
	s := Series{Label: "Sleep", Period: "day", Values: []float64{7, 7, 7, 0}, Labels: []string{"a", "b", "c", "d"}}

	got := strings.Join(Find(Input{Series: []Series{s}}, 3), " | ")
	if !strings.Contains(got, "days") {
		t.Errorf("a three-day run is not pluralised: %s", got)
	}
}

func TestFindRoundsToTheMetricsPrecision(t *testing.T) {
	// Calories are counted in whole numbers; "610.0kcal" implies a scale
	// nobody measured to.
	s := Series{
		Label: "Burn", Unit: "kcal", Decimals: 0, Period: "day",
		Values: []float64{100, 100, 610}, Labels: []string{"a", "b", "c"},
	}

	got := strings.Join(Find(Input{Series: []Series{s}}, 3), " | ")
	if strings.Contains(got, "610.0") {
		t.Errorf("rendered a spurious decimal: %s", got)
	}
	if !strings.Contains(got, "610kcal") {
		t.Errorf("did not render the value as a whole number: %s", got)
	}
}

func TestFindKeepsADecimalWhereItMatters(t *testing.T) {
	// The difference between 7.0h and 7.4h is the whole point of the metric.
	s := Series{
		Label: "Sleep", Unit: "h", Decimals: 1, Period: "day",
		Values: []float64{5, 5, 7.4}, Labels: []string{"a", "b", "c"},
	}

	got := strings.Join(Find(Input{Series: []Series{s}}, 3), " | ")
	if !strings.Contains(got, "7.4h") {
		t.Errorf("dropped a meaningful decimal: %s", got)
	}
}
