package insights

import (
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/activity/activity"
	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/habits"
	"github.com/NorthAIProject/north-client/internal/hydration"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/sleep"
	insightpages "github.com/NorthAIProject/north-client/web/insights"
)

// wellLogged is a week with something in every domain.
func wellLogged(t *testing.T) SummaryData {
	t.Helper()
	rg := weekRange(t)
	day := func(n int) time.Time { return rg.Since.AddDate(0, 0, n) }

	return SummaryData{
		Range: rg,
		Body: BodyData{
			Range:         rg,
			HydrationGoal: hydration.DefaultDailyTargetML,
			Hydration: []hydration.Day{
				{Date: day(0), TotalML: 2000, Entries: 2, TargetML: 2000},
				{Date: day(1), TotalML: 2000, Entries: 2, TargetML: 2000},
			},
			Nights: []sleep.Log{
				{LocalDate: day(0), DurationMinutes: 480, Bedtime: "23:00"},
				{LocalDate: day(1), DurationMinutes: 480, Bedtime: "23:10"},
				{LocalDate: day(2), DurationMinutes: 470, Bedtime: "22:55"},
			},
			Habits: []habits.Stats{{Kept: 6, Scheduled: 7}},
		},
		Mind: MindData{
			Range: rg,
			CheckIns: []checkins.CheckIn{
				{LocalDate: day(0), Mood: 4, Energy: 4},
				{LocalDate: day(1), Mood: 5, Energy: 4},
			},
		},
		Progress: ProgressData{
			Range:  rg,
			Active: []goals.Goal{{}, {}},
			Notes:  []goals.TimelineUpdate{{}},
			Streak: 5,
		},
		Training: TrainingData{
			Range: rg,
			Sessions: []activity.Session{
				{StartedAt: day(0)}, {StartedAt: day(3)}, {StartedAt: day(5)},
			},
			Calories: 900,
			Prior:    800,
		},
	}
}

func quarterRange(t *testing.T) timerange.Range {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	return timerange.Parse(timerange.KeyQuarter, loc)
}

func mustSummaryView(t *testing.T, data SummaryData) insightpages.SummaryView {
	t.Helper()
	view, err := buildSummaryView(data)
	if err != nil {
		t.Fatalf("buildSummaryView: %v", err)
	}
	return view
}

func find(t *testing.T, view insightpages.SummaryView, key string) insightpages.ScoreCard {
	t.Helper()
	for _, s := range view.Scores {
		if s.Key == key {
			return s
		}
	}
	t.Fatalf("no score card %q", key)
	return insightpages.ScoreCard{}
}

func TestSummaryViewScoresEveryDomain(t *testing.T) {
	view := mustSummaryView(t, wellLogged(t))

	if view.Empty {
		t.Fatal("Empty = true on a well-logged week")
	}
	if len(view.Scores) != 5 {
		t.Fatalf("scores = %d, want 5", len(view.Scores))
	}
	// The fixture logs everything except food, so nutrition is deliberately
	// absent here — TestSummaryScoresNutritionWhenThereIsAPlan covers it.
	for _, want := range []string{"body", "mind", "progress", "training"} {
		if c := find(t, view, want); !c.HasData {
			t.Errorf("%s has no data on a well-logged week", want)
		}
	}
	if view.Judged != 4 {
		t.Errorf("Judged = %d, want 4 — nutrition has nothing logged", view.Judged)
	}
}

func TestSummaryViewOfAnEmptyWindowSaysSoOnce(t *testing.T) {
	// Four empty rings and a row of dashes is worse than one sentence.
	view := mustSummaryView(t, SummaryData{Range: weekRange(t)})

	if !view.Empty {
		t.Error("Empty = false with nothing logged anywhere")
	}
	if view.Judged != 0 {
		t.Errorf("Judged = %d, want 0", view.Judged)
	}
}

func TestSummaryViewCountsOnlyJudgedDomainsAsOnTrack(t *testing.T) {
	data := wellLogged(t)
	data.Training = TrainingData{Range: data.Range} // nothing trained

	view := mustSummaryView(t, data)

	if c := find(t, view, "training"); c.HasData {
		t.Error("training scored despite no sessions")
	}
	if view.Judged != 3 {
		t.Errorf("Judged = %d, want 3 — an unscored domain is not judged", view.Judged)
	}
	if view.OnTrack > view.Judged {
		t.Errorf("OnTrack %d > Judged %d", view.OnTrack, view.Judged)
	}
}

func TestSummaryViewCardsLinkToTheirSection(t *testing.T) {
	view := mustSummaryView(t, wellLogged(t))

	if got := find(t, view, "body").Href; got != "/app/insights/body" {
		t.Errorf("body href = %q, want %q", got, "/app/insights/body")
	}
}

func TestSummaryViewLabelsEveryComponent(t *testing.T) {
	// A breakdown row reading "sleep_duration 40/40" is a leaked identifier.
	view := mustSummaryView(t, wellLogged(t))

	for _, s := range view.Scores {
		for _, c := range s.Components {
			if c.Label == "" {
				t.Errorf("%s has an unlabelled component", s.Key)
			}
		}
	}
}

func TestSummaryViewPinsAHeadlineFromEachDomain(t *testing.T) {
	view := mustSummaryView(t, wellLogged(t))

	if len(view.Pinned) == 0 {
		t.Fatal("no pinned cards on a well-logged week")
	}
	for _, c := range view.Pinned {
		if c.Value == "" {
			t.Errorf("pinned card %q has no value", c.Label)
		}
		if c.Href == "" {
			t.Errorf("pinned card %q has no destination", c.Label)
		}
	}
}

func TestSummaryViewSurfacesHighlights(t *testing.T) {
	// wellLogged burns 900 kcal against a prior window's 800 — a 12% rise,
	// which is over the threshold the rules stay silent below.
	view := mustSummaryView(t, wellLogged(t))

	if len(view.Highlights) == 0 {
		t.Fatal("no highlights on a window with a 12% rise in burn")
	}
	for _, line := range view.Highlights {
		if line == "" {
			t.Error("an empty highlight line")
		}
	}
}

func TestSummaryViewOfAFlatWindowClaimsNothing(t *testing.T) {
	data := wellLogged(t)
	data.Training.Calories = 800
	data.Training.Prior = 800

	view := mustSummaryView(t, data)

	for _, line := range view.Highlights {
		if strings.Contains(line, "than the window before") {
			t.Errorf("claimed a change where there was none: %q", line)
		}
	}
}

func TestSummaryPinnedCardsOpenTheirMetricPage(t *testing.T) {
	// Apple's pinned cards drill into the metric, not into the category. A
	// card that lands on the domain page makes the reader hunt for the number
	// they just tapped.
	view := mustSummaryView(t, wellLogged(t))

	for _, c := range view.Pinned {
		if !strings.HasPrefix(c.Href, "/app/insights/metric/") {
			t.Errorf("pinned card %q links to %q, want a metric page", c.Label, c.Href)
			continue
		}
		key := strings.TrimPrefix(c.Href, "/app/insights/metric/")
		if _, ok := lookupMetric(key); !ok {
			t.Errorf("pinned card %q links to %q, which is not a registered metric", c.Label, key)
		}
	}
}

func TestSummaryShowsADayTableForAShortWindow(t *testing.T) {
	// Apple's activity view puts one row per day beside the rings. It only
	// works while the window is short enough to list.
	data := wellLogged(t)
	view := mustSummaryView(t, data)

	if !view.HasDays {
		t.Fatal("HasDays = false for a seven-day window")
	}
	if want := len(data.Range.Buckets()); len(view.Days) != want {
		t.Errorf("%d day rows for %d buckets", len(view.Days), want)
	}
	for _, d := range view.Days {
		if d.Label == "" {
			t.Error("a day row has no label")
		}
		if len(d.Cells) != 3 {
			t.Errorf("day %q has %d cells, want 3", d.Label, len(d.Cells))
		}
	}
}

func TestSummaryHidesTheDayTableForALongWindow(t *testing.T) {
	// Ninety rows is not a table anybody reads.
	data := wellLogged(t)
	data.Range = quarterRange(t)

	view := mustSummaryView(t, data)

	if view.HasDays {
		t.Error("HasDays = true for a quarter — that is a wall of rows, not a table")
	}
}

func TestSummaryDayTableMarksLoggedAndUnloggedDays(t *testing.T) {
	view := mustSummaryView(t, wellLogged(t))

	var filled, empty int
	for _, d := range view.Days {
		for _, c := range d.Cells {
			if c.Filled {
				filled++
			} else {
				empty++
			}
		}
	}
	if filled == 0 {
		t.Error("no cell is marked logged despite a well-logged week")
	}
	if empty == 0 {
		t.Error("no cell is marked unlogged despite gaps in the fixture")
	}
}

func TestSummaryScoresNutritionWhenThereIsAPlan(t *testing.T) {
	data := wellLogged(t)
	data.Nutrition = NutritionData{
		Range:   data.Range,
		HasGoal: true,
		Goal:    calculator.MacroPlan{CalorieGoal: 2400, ProteinG: 180},
		Days: []NutritionDay{
			{Date: data.Range.Since, Macros: meals.Macros{Calories: 2400, ProteinG: 180}},
			{Date: data.Range.Since.AddDate(0, 0, 1), Macros: meals.Macros{Calories: 2350, ProteinG: 175}},
		},
	}

	view := mustSummaryView(t, data)

	c := find(t, view, "nutrition")
	if !c.HasData {
		t.Error("nutrition did not score despite a plan and two logged days")
	}
	if c.Href == "" {
		t.Error("the nutrition ring links nowhere")
	}
}

func TestSummaryLeavesNutritionUnscoredWithoutFoodLogs(t *testing.T) {
	view := mustSummaryView(t, wellLogged(t))

	if find(t, view, "nutrition").HasData {
		t.Error("nutrition scored with no food logged")
	}
}
