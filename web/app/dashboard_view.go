package app

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/goals/goal"
	"github.com/NorthAIProject/north-client/internal/nudges/nudge"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
	"github.com/NorthAIProject/north-client/web/shared/ui/chart"
)

// DashboardData is the command-center snapshot.
type DashboardData struct {
	// NewsTickerEnabled renders the breaking-news strip's lazy shell.
	NewsTickerEnabled bool

	Range RangeView

	CheckedInToday  bool
	Streak          int
	GoalActivity7d  int
	PendingMemories int
	Goals           []goal.Goal
	LastThread      *conversations.Conversation
	NextSession     *plan.PlanDay
	PlanID          uuid.UUID
	// DoneToday is today's plan session when it is already finished;
	// NextSession has then moved past it. SessionToday is NextSession
	// falling on the reader's local today. Both are decided by the service,
	// which knows the reader's time zone; the template does not.
	DoneToday        *plan.PlanDay
	SessionToday     bool
	CheckIns         CheckInSeriesView
	Habits           HabitsView
	Hydration        HydrationView
	Sleep            SleepView
	ActivityCalories float64
	Timeline         []TimelineEntryView
	Deltas           DeltasView

	MoodChart        chart.Props
	HydrationChart   chart.Props
	HabitGaugeOption map[string]any
	CheckInHeatmap   map[string]any
	ActivityDonut    map[string]any
	HasActivityDonut bool

	Nudges []nudge.Nudge

	// Briefing is this morning's note, already rendered markdown source. Empty
	// when there is none for today.
	Briefing    string
	HasBriefing bool

	// HasNextStep is the first-run card. Hidden once the person has a goal,
	// today's check-in, and a coach thread.
	HasNextStep bool
	NextStep    NextStep
}

// NextStep is the one action a fresh account should take next.
type NextStep struct {
	// Kind is the step's identity from internal/dashboard. The template reads
	// it for exactly one case: NextStepKindPush hides itself when the browser
	// cannot deliver notifications, which only script can know.
	Kind    string
	Eyebrow string
	Title   string
	Body    string
	CTA     string
	Href    string
}

// RangeOption is one choice in the period selector.
type RangeOption struct {
	Key      string
	Label    string
	Selected bool
}

// LabelIn is the option's label in the request's language. Key is
// timerange.Key, so the catalogue entry is derivable rather than carried.
func (o RangeOption) LabelIn(ctx context.Context) string {
	return rangeLabel(ctx, o.Key, o.Label)
}

type RangeView struct {
	Key     string
	Label   string
	Options []RangeOption
}

// LabelIn is the selected range's label in the request's language.
func (v RangeView) LabelIn(ctx context.Context) string {
	return rangeLabel(ctx, v.Key, v.Label)
}

// rangeLabel translates a range key, falling back to the English the handler
// carried rather than to the key: a range this build stops naming should still
// read as a period, not as "range.fortnight".
func rangeLabel(ctx context.Context, key, english string) string {
	if key == "" {
		return english
	}
	if got := i18n.T(ctx, "range."+key); got != "range."+key {
		return got
	}
	return english
}

// DeltaView is a metric's change against the previous window of equal length.
//
// HasPrior false means there is nothing to compare against. The template then
// renders nothing at all: a first-week user shown "+100%" on every tile is
// being told something false.
type DeltaView struct {
	Pct       float64
	Direction int
	HasPrior  bool
}

type DeltasView struct {
	Hydration  DeltaView
	SleepHours DeltaView
	Calories   DeltaView
	CheckIns   DeltaView
}

// TimelineEntryView is one row of the activity feed.
type TimelineEntryView struct {
	Kind   string
	Label  string
	At     time.Time
	Title  string
	Detail string
	Href   string
	Icon   string
}

type CheckInDayView struct {
	Label  string
	Mood   int
	Energy int
}

type CheckInSeriesView struct {
	Days []CheckInDayView
}

func (s CheckInSeriesView) HasData() bool {
	for _, d := range s.Days {
		if d.Mood > 0 || d.Energy > 0 {
			return true
		}
	}
	return false
}

type HabitsView struct {
	HasHabits  bool
	Rate       int
	Kept       int
	Scheduled  int
	BestStreak int
}

type HydrationDayView struct {
	Label    string
	TotalML  int
	TargetML int
}

type HydrationView struct {
	TodayML  int
	TargetML int
	Percent  int
	Days     []HydrationDayView
}

func (h HydrationView) HasData() bool {
	if h.TodayML > 0 {
		return true
	}
	for _, d := range h.Days {
		if d.TotalML > 0 {
			return true
		}
	}
	return false
}

type SleepView struct {
	Logged          bool
	DurationMinutes int
	Quality         *int
}

func (d DashboardData) HasPlan() bool {
	return d.PlanID != uuid.Nil
}

func hasProgress(g goal.Goal) bool {
	_, ok := g.Progress()
	return ok
}

func progressPct(g goal.Goal) int {
	pct, _ := g.Progress()
	return pct
}

func formatLitres(ml int) string {
	if ml >= 1000 {
		return fmt.Sprintf("%.1f L", float64(ml)/1000)
	}
	return fmt.Sprintf("%d ml", ml)
}

func formatSleep(minutes int) string {
	h := minutes / 60
	m := minutes % 60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

// rangeHref keeps the selector working without JavaScript.
func rangeHref(key string) string {
	return "/app/overview?range=" + key
}

func panelsHref(key string) string {
	return "/app/overview/panels?range=" + key
}

// relativeTime keeps a feed row short. Anything older than a week is a date,
// because "9 days ago" is harder to place than "4 Aug".
func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("2 Jan")
	}
}

// NextStepKindPush matches dashboard.StepKindPush. Repeated here rather than
// imported so the view package does not depend on the slice that renders it.
const NextStepKindPush = "push"

func formatPct(pct float64) string {
	if pct < 0 {
		pct = -pct
	}
	return fmt.Sprintf("%.0f%%", pct)
}

// TodayItem is one thing the reader can still do today. The Today card lists
// them in a fixed order and names the first one not done as the next step, so
// the page answers "what now" before it shows any chart.
type TodayItem struct {
	Key    string
	Icon   string
	Label  string
	Detail string
	Href   string
	CTA    string
	Done   bool
}

// Today keys, for tests and for anything that needs to find one item.
const (
	TodayCheckIn = "checkin"
	TodayTrain   = "train"
	TodayWater   = "water"
	TodaySleep   = "sleep"
)

// todayItems is today's list. An item appears only when it can be done today:
// training only on a plan day, water only once there is a target to reach.
// Check-in and sleep are always there, because both are daily for everyone.
func todayItems(ctx context.Context, data DashboardData) []TodayItem {
	items := []TodayItem{checkInItem(ctx, data)}
	switch {
	case data.DoneToday != nil:
		items = append(items, trainItem(ctx, data, *data.DoneToday, true))
	case data.SessionToday && data.NextSession != nil:
		items = append(items, trainItem(ctx, data, *data.NextSession, false))
	}
	if data.Hydration.TargetML > 0 {
		items = append(items, TodayItem{
			Key:    TodayWater,
			Icon:   "droplet",
			Label:  i18n.T(ctx, "dash.today.water"),
			Detail: waterKPI(data),
			Href:   "/app/care",
			CTA:    i18n.T(ctx, "dash.kpi.logwater"),
			Done:   data.Hydration.TodayML >= data.Hydration.TargetML,
		})
	}
	sleepDetail := i18n.T(ctx, "dash.today.sleep.none")
	if data.Sleep.Logged {
		sleepDetail = sleepKPI(data)
	}
	items = append(items, TodayItem{
		Key:    TodaySleep,
		Icon:   "moon",
		Label:  i18n.T(ctx, "dash.today.sleep"),
		Detail: sleepDetail,
		Href:   "/app/care",
		CTA:    i18n.T(ctx, "dash.kpi.logsleep"),
		Done:   data.Sleep.Logged,
	})
	return items
}

func checkInItem(ctx context.Context, data DashboardData) TodayItem {
	item := TodayItem{
		Key:    TodayCheckIn,
		Icon:   "smile",
		Label:  i18n.T(ctx, "dash.today.checkin"),
		Detail: i18n.T(ctx, "dash.today.checkin.detail"),
		Href:   "/app/check-ins",
		CTA:    i18n.T(ctx, "dash.kpi.checkcta"),
		Done:   data.CheckedInToday,
	}
	if data.Streak > 0 {
		item.Detail = i18n.Tf(ctx, "dash.today.streak", streakKPI(ctx, data))
	}
	return item
}

func trainItem(ctx context.Context, data DashboardData, day plan.PlanDay, done bool) TodayItem {
	focus := day.Focus
	if focus == "" {
		focus = day.Weekday
	}
	detail := i18n.Tf(ctx, "dash.today.train.count", len(day.Exercises))
	if day.StartTime != "" {
		detail = day.StartTime + " · " + detail
	}
	return TodayItem{
		Key:    TodayTrain,
		Icon:   "dumbbell",
		Label:  i18n.Tf(ctx, "dash.today.train", focus),
		Detail: detail,
		Href:   "/app/training/" + data.PlanID.String(),
		CTA:    i18n.T(ctx, "dash.training.session"),
		Done:   done,
	}
}

// nextTodayItem is the first item not yet done, in list order.
func nextTodayItem(items []TodayItem) (TodayItem, bool) {
	for _, item := range items {
		if !item.Done {
			return item, true
		}
	}
	return TodayItem{}, false
}

func doneCount(items []TodayItem) int {
	n := 0
	for _, item := range items {
		if item.Done {
			n++
		}
	}
	return n
}

// showToday hides the Today card behind the first-run card. A fresh account
// has one thing to do, and a list beside it would compete with it. The push
// step is the exception: it stays hidden until script confirms the browser can
// deliver, and the page must not be left with neither card.
func (d DashboardData) showToday() bool {
	return !d.HasNextStep || d.NextStep.Kind == NextStepKindPush
}

// An em dash for no streak, and two catalogue strings for the rest: "1 days"
// is wrong in every language here.
func streakKPI(ctx context.Context, data DashboardData) string {
	switch {
	case data.Streak <= 0:
		return "—"
	case data.Streak == 1:
		return i18n.T(ctx, "dash.kpi.streak.one")
	default:
		return i18n.Tf(ctx, "dash.kpi.streak.many", data.Streak)
	}
}

func goalActivityLabel(n int) string {
	switch {
	case n <= 0:
		return ""
	case n == 1:
		return "1 this week"
	default:
		return strconv.Itoa(n) + " this week"
	}
}

func waterKPI(data DashboardData) string {
	if data.Hydration.TargetML <= 0 && data.Hydration.TodayML <= 0 {
		return "—"
	}
	return fmt.Sprintf("%s / %s", formatLitres(data.Hydration.TodayML), formatLitres(data.Hydration.TargetML))
}

func sleepKPI(data DashboardData) string {
	if !data.Sleep.Logged {
		return "—"
	}
	text := formatSleep(data.Sleep.DurationMinutes)
	if data.Sleep.Quality != nil {
		text += fmt.Sprintf(" · %d/5", *data.Sleep.Quality)
	}
	return text
}

func caloriesKPI(data DashboardData) string {
	if data.ActivityCalories <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f kcal", data.ActivityCalories)
}

// The habits and hydration panels describe the chart when there is one and
// what the panel is for when there is not.
func habitsDescription(ctx context.Context, data DashboardData) string {
	if data.Habits.HasHabits {
		return i18n.T(ctx, "dash.habits.desc")
	}
	return i18n.T(ctx, "dash.habits.empty")
}

func hydrationDescription(ctx context.Context, data DashboardData) string {
	if data.Hydration.HasData() {
		return i18n.T(ctx, "dash.hydration.desc")
	}
	return i18n.T(ctx, "dash.hydration.empty")
}
