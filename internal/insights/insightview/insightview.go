// Package insightview holds the views the Insights pages are drawn from: one
// struct per page, each built by internal/insights from the person's data.
//
// The browser's templates in web/insights render them, and the JSON API
// returns several of them as they are, so the types live here rather than in
// the templates. internal/insights imports web/insights for its browser
// handlers, so a view declared in internal/insights itself could not be
// imported back by the templates — hence a package of its own.
package insightview

import (
	"fmt"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/viz"
	"github.com/NorthAIProject/north-client/internal/workouts/workoutsummary"
)

// RangeOption is one choice in the period selector.
type RangeOption struct {
	Key      string
	Label    string
	Selected bool
}

// RangeView is the window a page covers and the choices its selector offers.
type RangeView struct {
	Key     string
	Label   string
	Options []RangeOption
}

// DeltaView is a change against the previous window of equal length.
// HasPrior false renders nothing at all, rather than a fabricated gain.
type DeltaView struct {
	Pct       float64
	Direction int
	HasPrior  bool
}

// ComponentRow is one weighted part of a score, shown the way a watch shows
// it: earned over available, not a percentage of a percentage.
type ComponentRow struct {
	Label  string
	Earned int
	Weight int

	// Known false renders a dash. A component nobody measured is not a zero,
	// and drawing it as one would be a claim about the person rather than
	// about the data.
	Known bool
}

// ScoreCard is one domain's ring, verdict and breakdown.
type ScoreCard struct {
	Key     string
	Label   string
	Icon    string
	Href    string
	Points  int
	Verdict string
	Reason  string

	Components []ComponentRow
	Coverage   int
	HasData    bool

	// Option is the ECharts gauge, decoded for templ.JSONScript. Nil when
	// there is nothing to draw.
	Option map[string]any
}

// PinnedCard is one headline number with its sparkline, mirroring the stack of
// cards on the Apple Health summary.
type PinnedCard struct {
	Label string
	Icon  string
	Value string
	Note  string
	Href  string
	Delta DeltaView

	// Chart is the card's sparkline. HasChart is false for a card whose
	// window holds nothing to draw — an axis with no series reads as a bug.
	Chart    viz.Props
	HasChart bool
}

// DayCell is one metric's mark for one day.
type DayCell struct {
	Label string
	Value string

	// Filled is whether anything was logged. The table shows a mark, not a
	// number, for the same reason a watch shows a ring: the question is
	// whether the day happened, and the number is on its own page.
	Filled bool
}

// DayRow is one day of the window.
type DayRow struct {
	Label string
	Cells []DayCell
}

// SummaryView is the whole page.
type SummaryView struct {
	Range      RangeView
	Scores     []ScoreCard
	Pinned     []PinnedCard
	Highlights []string

	// OnTrack counts the domains scoring OK or better, over the domains that
	// had enough data to judge. Shown instead of blending the four into one
	// number: a single figure nobody can decompose is a figure nobody can act
	// on, which is why Apple does not compute one either.
	OnTrack int
	Judged  int

	// Days is one row per day, the way the activity view lists a week.
	// HasDays is false for a window too long to list — ninety rows is a wall,
	// not a table.
	Days    []DayRow
	HasDays bool

	// Empty marks an account with nothing logged anywhere, which gets a
	// sentence rather than four empty rings.
	Empty bool
}

// TrendView is the row that says which way a metric is going.
//
// Direction is separate from Word so the arrow and the sentence cannot
// disagree, and HasPrior gates both: a first window is not an improvement on
// anything, and saying "steady" about it would be a claim with no evidence.
type TrendView struct {
	Direction int
	Pct       float64
	Word      string
	HasPrior  bool
}

// ComparisonView is this window against the one before it, as two bars.
// Percentages are of the larger of the two, so the longer bar always fills.
type ComparisonView struct {
	CurrentLabel string
	CurrentValue string
	CurrentPct   int

	PriorLabel string
	PriorValue string
	PriorPct   int

	HasPrior bool
}

// MetricView is one metric's page.
type MetricView struct {
	Range RangeView

	// Key is the metric's URL segment, needed so the range selector swaps
	// this metric's fragment rather than whichever one the range is called.
	Key      string
	Label    string
	Headline string
	Note     string

	// Href is the domain page this metric belongs to.
	Href      string
	HrefLabel string

	Chart      viz.Props
	Trend      TrendView
	Comparison ComparisonView
	Highlights []string

	HasData bool

	// Health marks a metric synced from a health provider.
	Health bool
	// Usual places the latest day against the person's own usual range; nil
	// until there are enough days for one.
	Usual *UsualView
}

// UsualView is a latest day against the weeks before it.
type UsualView struct {
	Day    time.Time
	Latest float64
	Mean   float64
	SD     float64
	// Low and High are the usual range, the mean give or take one standard
	// deviation, already formatted at the metric's precision.
	Low, High string
	Days      int
	Z         float64
	State     string // "above", "usual" or "below"
	Text      string
}

// HabitRow is one habit's adherence over the window.
type HabitRow struct {
	Name      string
	Kept      int
	Scheduled int
	Streak    int
	Rate      int
}

// BodyView is Insights → Body: water, sleep and habits over the window.
type BodyView struct {
	Range RangeView

	WaterChart   viz.Props
	SleepChart   viz.Props
	HabitGauge   map[string]any
	Habits       []HabitRow
	TotalWaterML int
	Nights       int
	AvgSleep     float64
	AvgQuality   float64

	// QualityCount is how many nights carried a rating. An average over two
	// of nine nights is a different claim from an average over nine, and the
	// page says which.
	QualityCount int

	HasWater  bool
	HasSleep  bool
	HasHabits bool
}

// MindView is Insights → Mind: check-ins and journal entries over the window.
type MindView struct {
	Range RangeView

	MoodChart    viz.Props
	JournalChart viz.Props
	MoodHeatmap  map[string]any

	CheckInCount int
	JournalCount int
	AvgMood      float64
	AvgEnergy    float64

	HasCheckIns bool
	HasJournal  bool
}

// NutritionView is the nutrition section.
type NutritionView struct {
	Range RangeView

	AvgCalories string
	AvgProtein  string
	DaysLogged  int
	Entries     int

	// Goal figures are empty for somebody who has not run the calculator.
	// Their intake is still charted; it is simply not judged against
	// anything, because there is nothing to judge it against.
	HasGoal      bool
	GoalCalories string
	GoalProtein  string

	CaloriesChart viz.Props
	MacroSplit    map[string]any
	HasSplit      bool

	Highlights []string
	HasData    bool

	Eating EatingView
}

// Row is a label and a value, for the small tables on the stats pages.
type Row struct {
	Label string
	Value string
	Note  string
}

// Bar is one horizontal bar: a label, a value, and its width in percent.
type Bar struct {
	Label string
	Value string
	Pct   int
}

// SleepView is Insights → Sleep over the window.
type SleepView struct {
	Range   RangeView
	HasData bool
	Tiles   []Row // headline numbers
	Nights  viz.Props
	// Stages is each stage's share of time asleep.
	Stages   []Bar
	Schedule []Row // bedtime, wake, consistency, weekday vs weekend
	Extremes []Row // best and worst night
}

// CardioView is Insights → Cardio: the window's sessions, runs and heart rate.
type CardioView struct {
	Range       RangeView
	HasData     bool
	Tiles       []Row
	Weekly      viz.Props
	WeeklyTitle string
	Kinds       []Bar
	Runs        []Row
	HasRuns     bool
	Recent      []Row
	Heart       []Row
}

// EatingView is the eating-patterns section of Insights → Nutrition.
type EatingView struct {
	HasData bool
	Tiles   []Row
	Slots   []Bar
	Foods   []Row
	Notes   []Row
}

// PatternView is one finding: a sentence and the two bars it compares.
type PatternView struct {
	Title  string
	Detail string
	A, B   Bar
}

// PatternsView is Insights → Patterns: the findings over the last Days days.
type PatternsView struct {
	Range    RangeView
	Days     int
	Findings []PatternView
}

// GoalRow is one active goal and where it stands.
type GoalRow struct {
	ID          string
	Title       string
	Progress    int
	HasProgress bool
	Deadline    string
	Overdue     bool
}

// ProgressView is Insights → Progress: goals, notes and streaks over the window.
type ProgressView struct {
	Range RangeView

	Goals       []GoalRow
	NotesChart  viz.Props
	StatusDonut map[string]any

	ActiveCount int
	AvgProgress int
	Overdue     int
	Streak      int
	NoteCount   int
	OpenedCount int

	HasDonut bool
	HasNotes bool
}

// SessionRow is one completed activity session.
type SessionRow struct {
	Name     string
	At       time.Time
	Duration string
	Calories float64
}

// TrainingView is Insights → Training: sessions, lifting and the plan over the
// window.
type TrainingView struct {
	Range RangeView

	Sessions  []SessionRow
	BurnChart viz.Props
	KindDonut map[string]any

	Calories     float64
	Delta        DeltaView
	SessionCount int
	TotalTime    string

	HasDonut    bool
	HasSessions bool

	Lifting LiftingView

	// Plan is the training plan over the window; zero without one.
	Plan PlanAdherenceView
	// Recap is the latest finished workout in the window; nil before one.
	Recap *workoutsummary.Recap
}

// PlanAdherenceView is the plan against what was done: this week day by day,
// and the window as a count.
type PlanAdherenceView struct {
	HasPlan bool
	Week    workoutsummary.Week

	Done     int
	Planned  int
	Sentence string
}

// Rate is the window's plan days done, as a percentage.
func (p PlanAdherenceView) Rate() string {
	if p.Planned == 0 {
		return "—"
	}
	return fmt.Sprintf("%d%%", p.Done*100/p.Planned)
}

// LiftingView is what the logged sets add up to.
type LiftingView struct {
	HasSets     bool
	Volume      string
	Delta       DeltaView
	Sets        int
	Workouts    int
	HasWeekly   bool
	WeeklyChart viz.Props
	Exercises   []LiftExerciseRow
	Records     []LiftRecordRow
	Muscles     []MuscleRow
}

// LiftExerciseRow is one exercise's sets, best and volume over the window.
type LiftExerciseRow struct {
	Name   string
	Sets   int
	Best   string
	E1RM   string
	Volume string
	// Change is the estimated max's movement across the window, in kg.
	Change float64
}

// LiftRecordRow is one personal record set in the window.
type LiftRecordRow struct {
	Exercise string
	Lift     string
	E1RM     string
	Gain     string
	On       string
}

// MuscleRow is one muscle's share of the window's sets.
type MuscleRow struct {
	Name string
	Sets int
	Pct  int
}

// CoachView is the coaching-activity section.
//
// There is no score ring here, unlike every other section. How much somebody
// talks to their coach is not a thing to be graded: a quiet week can be a good
// week, and a number that fell would read as a reprimand for living.
type CoachView struct {
	Range RangeView

	Turns        int
	YourMessages int
	CoachReplies int

	// HelpfulRate is over rated replies alone. An unrated reply is not a
	// thumbs-down, and counting it as one would make the coach look worse the
	// more it was used.
	HasRatings  bool
	HelpfulRate int
	Rated       int

	Chart      viz.Props
	Highlights []string

	Truncated bool
	HasData   bool
}

// SpendRow is one line of a cost breakdown.
type SpendRow struct {
	Label       string
	Cost        string
	Generations int64
	Tokens      int64

	// Pct is this row's share of the window's cost, for the inline bar. Zero
	// across the board in a window that cost nothing, which is a real state
	// now that transcription runs on the cluster's own service.
	Pct int
}

// SpendView is the model-cost section.
type SpendView struct {
	Range RangeView

	TotalCost   string
	TotalTokens int64
	Generations int64

	Surfaces []SpendRow
	Models   []SpendRow

	HasData bool
}

// EntryRow is one row of the full activity feed.
type EntryRow struct {
	Kind   string
	Label  string
	At     time.Time
	Title  string
	Detail string
	Href   string
	Icon   string
}

// FilterChip narrows the feed to one kind. Count is shown so an empty result
// is obviously a filter and not a missing record.
type FilterChip struct {
	Key      string
	Label    string
	Count    int
	Selected bool
}

// TimelineView is Insights → Timeline: the entries in the window and the chips
// that filter them.
type TimelineView struct {
	Range   RangeView
	Entries []EntryRow
	Chips   []FilterChip

	// Overflow marks a window with more rows than one page holds.
	Overflow bool
}
