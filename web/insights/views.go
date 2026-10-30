package insights

import "github.com/NorthAIProject/north-client/internal/insights/insightview"

// The views these pages draw are declared in internal/insights/insightview,
// where the JSON API can use them without importing the templates. These
// aliases keep the pages' signatures as they were.
type (
	RangeOption       = insightview.RangeOption
	RangeView         = insightview.RangeView
	DeltaView         = insightview.DeltaView
	ComponentRow      = insightview.ComponentRow
	ScoreCard         = insightview.ScoreCard
	PinnedCard        = insightview.PinnedCard
	DayCell           = insightview.DayCell
	DayRow            = insightview.DayRow
	SummaryView       = insightview.SummaryView
	TrendView         = insightview.TrendView
	ComparisonView    = insightview.ComparisonView
	MetricView        = insightview.MetricView
	UsualView         = insightview.UsualView
	HabitRow          = insightview.HabitRow
	BodyView          = insightview.BodyView
	MindView          = insightview.MindView
	NutritionView     = insightview.NutritionView
	Row               = insightview.Row
	Bar               = insightview.Bar
	SleepView         = insightview.SleepView
	CardioView        = insightview.CardioView
	EatingView        = insightview.EatingView
	PatternView       = insightview.PatternView
	PatternsView      = insightview.PatternsView
	GoalRow           = insightview.GoalRow
	ProgressView      = insightview.ProgressView
	SessionRow        = insightview.SessionRow
	TrainingView      = insightview.TrainingView
	PlanAdherenceView = insightview.PlanAdherenceView
	LiftingView       = insightview.LiftingView
	LiftExerciseRow   = insightview.LiftExerciseRow
	LiftRecordRow     = insightview.LiftRecordRow
	MuscleRow         = insightview.MuscleRow
	CoachView         = insightview.CoachView
	SpendRow          = insightview.SpendRow
	SpendView         = insightview.SpendView
	EntryRow          = insightview.EntryRow
	FilterChip        = insightview.FilterChip
	TimelineView      = insightview.TimelineView
)
