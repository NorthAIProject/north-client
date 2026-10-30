package insights

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/insights/insightview"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
	"github.com/NorthAIProject/north-client/internal/shared/viz"
	"github.com/NorthAIProject/north-client/internal/stats/stat"
	"github.com/NorthAIProject/north-client/internal/users"
)

func TestInsightsShapes(t *testing.T) {
	t.Parallel()

	rng := insightview.RangeView{Key: "week", Label: "Last 7 days", Options: []insightview.RangeOption{
		{Key: "week", Label: "Last 7 days", Selected: true}, {Key: "month", Label: "Last 30 days"},
	}}
	week := viz.Data{Labels: []string{"Mon", "Tue", "Wed"}, Datasets: []viz.Dataset{{Label: "Sleep", Data: []float64{7.2, 6.8, 7.9}}}}

	apitest.AssertGolden(t, "insights-summary.golden.json", projectSummary(insightview.SummaryView{
		Range: rng,
		Scores: []insightview.ScoreCard{{
			Key: "body", Label: "Body", Points: 72, Verdict: "Good", Reason: "Sleep steady, water short on two days.",
			Coverage: 80, HasData: true,
			Components: []insightview.ComponentRow{{Label: "Sleep", Earned: 30, Weight: 40, Known: true}},
		}},
		Pinned: []insightview.PinnedCard{{
			Label: "Sleep", Value: "7.3 h", Note: "average a night", Href: metricHref("sleep"),
			Delta: insightview.DeltaView{Pct: 4.5, Direction: 1, HasPrior: true},
			Chart: viz.Props{Data: week}, HasChart: true,
		}},
		Highlights: []string{"Best night: Wednesday, 7.9 h."},
		OnTrack:    1, Judged: 1,
		Days: []insightview.DayRow{{Label: "Mon 6", Cells: []insightview.DayCell{
			{Label: "Sleep", Value: "7.2h", Filled: true}, {Label: "Water", Value: "—", Filled: false},
		}}},
		HasDays: true,
	}))
	apitest.AssertGolden(t, "insights-metric.golden.json", projectMetric(insightview.MetricView{
		Range: rng, Key: "sleep", Label: "Sleep", Headline: "7.3 h a night", Note: "From your sleep log.",
		Chart:      viz.Props{Data: week},
		Trend:      insightview.TrendView{Direction: 1, Pct: 4.5, Word: "up", HasPrior: true},
		Comparison: insightview.ComparisonView{CurrentLabel: "This week", CurrentValue: "7.3 h", CurrentPct: 100, PriorLabel: "Last week", PriorValue: "7.0 h", PriorPct: 96, HasPrior: true},
		Highlights: []string{"Up 4.5% on last week."},
		HasData:    true,
	}))
	usual := insightview.UsualView{
		Day: time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC), Latest: 11200, Mean: 9000, SD: 1100,
		Low: "7900", High: "10100", Days: 28, Z: 2, State: "above",
		Text: "Above your usual: 11200 on Thu 24 Sep, usually 7900–10100.",
	}
	apitest.AssertGolden(t, "insights-metric-health.golden.json", projectMetric(insightview.MetricView{
		Range: rng, Key: "steps", Label: "Steps", Headline: "9400", Note: "average across 7 logged days",
		Chart:   viz.Props{Data: week},
		Trend:   insightview.TrendView{Direction: 1, Pct: 6, Word: "Up 6%", HasPrior: true},
		HasData: true, Health: true, Usual: &usual,
	}))
	rough := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	roughHealth, roughNights := roughMorning(rough)
	recovery, err := (&Service{health: roughHealth, stats: roughNights}).Recovery(context.Background(), users.User{Timezone: "UTC"}, rough.Add(8*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	apitest.AssertGolden(t, "insights-recovery.golden.json", projectRecovery(context.Background(), recovery))
	thursday := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	trainedOn := map[time.Time]bool{thursday.AddDate(0, 0, -1): true, thursday.AddDate(0, 0, -2): true, thursday.AddDate(0, 0, -9): true}
	checkedOn := map[time.Time]bool{thursday: true, thursday.AddDate(0, 0, -9): true}
	apitest.AssertGolden(t, "insights-consistency.golden.json", projectConsistency(context.Background(), ConsistencyData{
		Consistency: stat.ConsistencyOf(trainedOn, checkedOn, thursday, 2), CheckInStreak: 1,
	}))
	apitest.AssertGolden(t, "insights-health.golden.json", projectHealth(context.Background(), []HealthRow{
		{
			Metric: metric{Key: "steps", Label: "Steps"},
			Recent: []point{{At: usual.Day.AddDate(0, 0, -1), Value: 8800}, {At: usual.Day, Value: 11200}},
			Usual: &Usual{
				Latest: point{At: usual.Day, Value: 11200}, Baseline: stat.Baseline{Mean: 9000, SD: 1100, Days: 28},
				Z: 2, State: stat.Above,
			},
		},
		{
			Metric: metric{Key: "vo2max", Label: "VO2 max", Decimals: 1},
			Recent: []point{{At: usual.Day, Value: 44.2}},
		},
	}))

	at := time.Date(2026, 9, 21, 7, 30, 0, 0, time.UTC)
	split := []viz.DonutSegment{{Label: "Active", Value: 2}, {Label: "Achieved", Value: 1}}

	apitest.AssertGolden(t, "insights-timeline.golden.json", projectTimeline(insightview.TimelineView{
		Range: rng,
		Entries: []insightview.EntryRow{{
			Kind: "check_in", Label: "Check-in", At: at, Title: "Mood 4, energy 3", Detail: "Slept well.",
			Href: "/app/check-ins", Icon: "smile",
		}},
		Chips: []insightview.FilterChip{
			{Key: "", Label: "Everything", Count: 1, Selected: true},
			{Key: "check_in", Label: "Check-ins", Count: 1},
		},
	}))
	apitest.AssertGolden(t, "insights-body.golden.json", projectBody(insightview.BodyView{
		Range: rng, WaterChart: viz.Props{Data: week}, SleepChart: viz.Props{Data: week},
		Habits:       []insightview.HabitRow{{Name: "Stretch", Kept: 5, Scheduled: 7, Streak: 3, Rate: 71}},
		TotalWaterML: 11200, Nights: 7, AvgSleep: 438, AvgQuality: 3.5, QualityCount: 4,
		HasWater: true, HasSleep: true, HasHabits: true,
	}, 71))
	apitest.AssertGolden(t, "insights-mind.golden.json", projectMind(insightview.MindView{
		Range: rng, JournalChart: viz.Props{Data: week},
		CheckInCount: 2, JournalCount: 3, AvgMood: 3.5, AvgEnergy: 3, HasCheckIns: true, HasJournal: true,
	}, []string{"Mon", "Tue", "Wed"}, []int{4, 0, 3}, []int{3, 0, 3}))
	apitest.AssertGolden(t, "insights-progress.golden.json", projectProgress(insightview.ProgressView{
		Range: rng, NotesChart: viz.Props{Data: week},
		Goals: []insightview.GoalRow{{
			ID: "0b7c6f2e-3f4a-4c1e-9a55-2d8e6b1f0c11", Title: "Run a 10k", Progress: 60, HasProgress: true,
			Deadline: "31 Oct", Overdue: false,
		}},
		ActiveCount: 2, AvgProgress: 45, Overdue: 0, Streak: 4, NoteCount: 3, OpenedCount: 1, HasNotes: true,
	}, split))
	apitest.AssertGolden(t, "insights-training.golden.json", projectTraining(insightview.TrainingView{
		Range: rng, BurnChart: viz.Props{Data: week},
		Sessions: []insightview.SessionRow{{Name: "Running", At: at, Duration: "42m", Calories: 410}},
		Calories: 410, Delta: insightview.DeltaView{Pct: 12.5, Direction: 1, HasPrior: true},
		SessionCount: 1, TotalTime: "42m", HasSessions: true,
		Lifting: insightview.LiftingView{
			HasSets: true, Volume: "4200 kg", Delta: insightview.DeltaView{Pct: 8, Direction: 1, HasPrior: true},
			Sets: 18, Workouts: 3, HasWeekly: true, WeeklyChart: viz.Props{Data: week},
			Exercises: []insightview.LiftExerciseRow{{Name: "Back squat", Sets: 6, Best: "100 kg", E1RM: "113.3 kg", Volume: "2400 kg", Change: 2.5}},
			Records:   []insightview.LiftRecordRow{{Exercise: "Back squat", Lift: "100 kg × 4", E1RM: "113.3 kg", Gain: "2.5 kg", On: "Mon, Sep 21"}},
			Muscles:   []insightview.MuscleRow{{Name: "Quads", Sets: 6, Pct: 100}},
		},
	}, []viz.DonutSegment{{Label: "Running", Value: 1}}))
	apitest.AssertGolden(t, "insights-nutrition.golden.json", projectNutrition(insightview.NutritionView{
		Range: rng, AvgCalories: "2150 kcal", AvgProtein: "140 g", DaysLogged: 5, Entries: 17,
		HasGoal: true, GoalCalories: "2300 kcal", GoalProtein: "150 g",
		CaloriesChart: viz.Props{Data: week}, HasSplit: true,
		Highlights: []string{"Highest: Tuesday, 2400 kcal."}, HasData: true,
		Eating: insightview.EatingView{
			HasData: true,
			Tiles:   []insightview.Row{{Label: "Average day", Value: "2150 kcal", Note: "140 g protein · 220 g carbs · 70 g fat"}},
			Slots:   []insightview.Bar{{Label: "Lunch", Value: "40%", Pct: 40}},
			Foods:   []insightview.Row{{Label: "Oats", Value: "×4", Note: "380 kcal"}},
			Notes:   []insightview.Row{{Label: "Days logged", Value: "5"}},
		},
	}, []viz.DonutSegment{{Label: "Protein", Value: 700}, {Label: "Fat", Value: 350}, {Label: "Carbs", Value: 1100}}))
	apitest.AssertGolden(t, "insights-coach.golden.json", projectCoach(insightview.CoachView{
		Range: rng, Turns: 12, YourMessages: 6, CoachReplies: 6, HasRatings: true, HelpfulRate: 80, Rated: 5,
		Chart: viz.Props{Data: week}, Highlights: []string{"Most messages: Tuesday."}, HasData: true,
	}))
	apitest.AssertGolden(t, "insights-spend.golden.json", projectSpend(insightview.SpendView{
		Range: rng, TotalCost: "€0.08", TotalTokens: 42000, Generations: 31,
		Surfaces: []insightview.SpendRow{{Label: "Coaching chat", Cost: "€0.06", Generations: 20, Tokens: 30000, Pct: 75}},
		Models:   []insightview.SpendRow{{Label: "claude-sonnet-5", Cost: "€0.06", Generations: 20, Tokens: 30000, Pct: 75}},
		HasData:  true,
	}))
}

func TestInsightsTrainingPlanAndRecapShape(t *testing.T) {
	t.Parallel()

	monday := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	data := TrainingData{
		Now:     monday.AddDate(0, 0, 2).Add(18 * time.Hour),
		HasPlan: true,
		Week: activity.Adherence{Planned: 3, Done: 1, Sentence: "1 of 3 sessions done this week.", Days: []activity.DayStatus{
			{Weekday: "Monday", Focus: "Push", Date: monday, Done: true},
			{Weekday: "Wednesday", Focus: "Legs", Date: monday.AddDate(0, 0, 2)},
			{Weekday: "Friday", Focus: "Pull", Date: monday.AddDate(0, 0, 4)},
		}},
		Adherence: activity.Adherence{Planned: 2, Done: 1, Sentence: "1 of 2 planned sessions done."},
		HasRecap:  true,
		Recap: lift.Recap{
			StartedAt: monday.Add(18 * time.Hour), PlanWeekday: "Monday", Focus: "Push",
			Sentence: "40 minutes, 3 of 3 sets, 720 kg.", Duration: 40 * time.Minute, SetsDone: 3, SetsPrescribed: 3, VolumeKg: 720,
			Exercises: []lift.RecapExercise{{Name: "Bench press", Sets: 3, VolumeKg: 720, Best: "60 kg × 4", E1RM: 68}},
		},
	}
	rng := insightview.RangeView{Key: "week", Label: "Last 7 days"}
	out := projectTraining(insightview.TrainingView{Range: rng}, nil)
	out.Plan, out.Recap = projectPlanWeek(context.Background(), data), projectRecap(data)
	apitest.AssertGolden(t, "insights-training-plan.golden.json", out)
}
