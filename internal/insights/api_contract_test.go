package insights

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
	"github.com/NorthAIProject/north-client/internal/shared/viz"
	insightpages "github.com/NorthAIProject/north-client/web/insights"
	"github.com/NorthAIProject/north-client/web/shared/ui/chart"
)

func TestInsightsShapes(t *testing.T) {
	t.Parallel()

	rng := insightpages.RangeView{Key: "week", Label: "Last 7 days", Options: []insightpages.RangeOption{
		{Key: "week", Label: "Last 7 days", Selected: true}, {Key: "month", Label: "Last 30 days"},
	}}
	week := chart.Data{Labels: []string{"Mon", "Tue", "Wed"}, Datasets: []chart.Dataset{{Label: "Sleep", Data: []float64{7.2, 6.8, 7.9}}}}

	apitest.AssertGolden(t, "insights-summary.golden.json", projectSummary(insightpages.SummaryView{
		Range: rng,
		Scores: []insightpages.ScoreCard{{
			Key: "body", Label: "Body", Points: 72, Verdict: "Good", Reason: "Sleep steady, water short on two days.",
			Coverage: 80, HasData: true,
			Components: []insightpages.ComponentRow{{Label: "Sleep", Earned: 30, Weight: 40, Known: true}},
		}},
		Pinned: []insightpages.PinnedCard{{
			Label: "Sleep", Value: "7.3 h", Note: "average a night", Href: metricHref("sleep"),
			Delta: insightpages.DeltaView{Pct: 4.5, Direction: 1, HasPrior: true},
			Chart: chart.Props{Data: week}, HasChart: true,
		}},
		Highlights: []string{"Best night: Wednesday, 7.9 h."},
		OnTrack:    1, Judged: 1,
	}))
	apitest.AssertGolden(t, "insights-metric.golden.json", projectMetric(insightpages.MetricView{
		Range: rng, Key: "sleep", Label: "Sleep", Headline: "7.3 h a night", Note: "From your sleep log.",
		Chart:      chart.Props{Data: week},
		Trend:      insightpages.TrendView{Direction: 1, Pct: 4.5, Word: "up", HasPrior: true},
		Comparison: insightpages.ComparisonView{CurrentLabel: "This week", CurrentValue: "7.3 h", CurrentPct: 100, PriorLabel: "Last week", PriorValue: "7.0 h", PriorPct: 96, HasPrior: true},
		Highlights: []string{"Up 4.5% on last week."},
		HasData:    true,
	}))

	at := time.Date(2026, 9, 21, 7, 30, 0, 0, time.UTC)
	split := []viz.DonutSegment{{Label: "Active", Value: 2}, {Label: "Achieved", Value: 1}}

	apitest.AssertGolden(t, "insights-timeline.golden.json", projectTimeline(insightpages.TimelineView{
		Range: rng,
		Entries: []insightpages.EntryRow{{
			Kind: "check_in", Label: "Check-in", At: at, Title: "Mood 4, energy 3", Detail: "Slept well.",
			Href: "/app/check-ins", Icon: "smile",
		}},
		Chips: []insightpages.FilterChip{
			{Key: "", Label: "Everything", Count: 1, Selected: true},
			{Key: "check_in", Label: "Check-ins", Count: 1},
		},
	}))
	apitest.AssertGolden(t, "insights-body.golden.json", projectBody(insightpages.BodyView{
		Range: rng, WaterChart: chart.Props{Data: week}, SleepChart: chart.Props{Data: week},
		Habits:       []insightpages.HabitRow{{Name: "Stretch", Kept: 5, Scheduled: 7, Streak: 3, Rate: 71}},
		TotalWaterML: 11200, Nights: 7, AvgSleep: 438, AvgQuality: 3.5, QualityCount: 4,
		HasWater: true, HasSleep: true, HasHabits: true,
	}, 71))
	apitest.AssertGolden(t, "insights-mind.golden.json", projectMind(insightpages.MindView{
		Range: rng, JournalChart: chart.Props{Data: week},
		CheckInCount: 2, JournalCount: 3, AvgMood: 3.5, AvgEnergy: 3, HasCheckIns: true, HasJournal: true,
	}, []string{"Mon", "Tue", "Wed"}, []int{4, 0, 3}, []int{3, 0, 3}))
	apitest.AssertGolden(t, "insights-progress.golden.json", projectProgress(insightpages.ProgressView{
		Range: rng, NotesChart: chart.Props{Data: week},
		Goals: []insightpages.GoalRow{{
			ID: "0b7c6f2e-3f4a-4c1e-9a55-2d8e6b1f0c11", Title: "Run a 10k", Progress: 60, HasProgress: true,
			Deadline: "31 Oct", Overdue: false,
		}},
		ActiveCount: 2, AvgProgress: 45, Overdue: 0, Streak: 4, NoteCount: 3, OpenedCount: 1, HasNotes: true,
	}, split))
	apitest.AssertGolden(t, "insights-training.golden.json", projectTraining(insightpages.TrainingView{
		Range: rng, BurnChart: chart.Props{Data: week},
		Sessions: []insightpages.SessionRow{{Name: "Running", At: at, Duration: "42m", Calories: 410}},
		Calories: 410, Delta: insightpages.DeltaView{Pct: 12.5, Direction: 1, HasPrior: true},
		SessionCount: 1, TotalTime: "42m", HasSessions: true,
	}, []viz.DonutSegment{{Label: "Running", Value: 1}}))
	apitest.AssertGolden(t, "insights-nutrition.golden.json", projectNutrition(insightpages.NutritionView{
		Range: rng, AvgCalories: "2150 kcal", AvgProtein: "140 g", DaysLogged: 5, Entries: 17,
		HasGoal: true, GoalCalories: "2300 kcal", GoalProtein: "150 g",
		CaloriesChart: chart.Props{Data: week}, HasSplit: true,
		Highlights: []string{"Highest: Tuesday, 2400 kcal."}, HasData: true,
	}, []viz.DonutSegment{{Label: "Protein", Value: 700}, {Label: "Fat", Value: 350}, {Label: "Carbs", Value: 1100}}))
	apitest.AssertGolden(t, "insights-coach.golden.json", projectCoach(insightpages.CoachView{
		Range: rng, Turns: 12, YourMessages: 6, CoachReplies: 6, HasRatings: true, HelpfulRate: 80, Rated: 5,
		Chart: chart.Props{Data: week}, Highlights: []string{"Most messages: Tuesday."}, HasData: true,
	}))
	apitest.AssertGolden(t, "insights-spend.golden.json", projectSpend(insightpages.SpendView{
		Range: rng, TotalCost: "€0.08", TotalTokens: 42000, Generations: 31,
		Surfaces: []insightpages.SpendRow{{Label: "Coaching chat", Cost: "€0.06", Generations: 20, Tokens: 30000, Pct: 75}},
		Models:   []insightpages.SpendRow{{Label: "claude-sonnet-5", Cost: "€0.06", Generations: 20, Tokens: 30000, Pct: 75}},
		HasData:  true,
	}))
}
