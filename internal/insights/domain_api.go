package insights

import (
	"context"
	"net/http"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/lifts"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/shared/viz"
	"github.com/NorthAIProject/north-client/internal/users"
	insightpages "github.com/NorthAIProject/north-client/web/insights"
)

// The domain pages behind the overview, one route each, projected from the
// same view models the web renders. Gauges and donuts go out as the numbers
// behind them (see habitAdherence and the *Segments helpers), not as chart
// configs.

// Segment is one slice of a split, such as goals by status.
type Segment struct {
	Label string `json:"label"`
	Value int    `json:"value"`
}

type TimelineEntry struct {
	Kind   string    `json:"kind"`
	Label  string    `json:"label"`
	At     time.Time `json:"at"`
	Title  string    `json:"title"`
	Detail string    `json:"detail"`
	// Href opens the entry's own page; empty when it has none.
	Href string `json:"href,omitempty"`
	// Icon is a lucide icon name, an open string.
	Icon string `json:"icon,omitempty"`
}

type TimelineFilter struct {
	// Key is the kind to pass back as ?kind=; empty means everything.
	Key      string `json:"key"`
	Label    string `json:"label"`
	Count    int    `json:"count"`
	Selected bool   `json:"selected"`
}

type InsightsTimeline struct {
	Range   Range            `json:"range"`
	Entries []TimelineEntry  `json:"entries"`
	Filters []TimelineFilter `json:"filters"`
	// Overflow means the window holds more entries than one page shows.
	Overflow bool `json:"overflow"`
}

type HabitStat struct {
	Name      string `json:"name"`
	Kept      int    `json:"kept"`
	Scheduled int    `json:"scheduled"`
	Streak    int    `json:"streak"`
	Rate      int    `json:"rate"`
}

type InsightsBody struct {
	Range Range `json:"range"`
	Water Chart `json:"water"`
	Sleep Chart `json:"sleep"`
	// Adherence is kept over scheduled across every habit, 0-100.
	Adherence       int         `json:"adherence"`
	Habits          []HabitStat `json:"habits"`
	TotalWaterML    int         `json:"totalWaterML"`
	Nights          int         `json:"nights"`
	AvgSleepMinutes float64     `json:"avgSleepMinutes"`
	AvgQuality      float64     `json:"avgQuality"`
	// QualityCount is how many nights carried a rating.
	QualityCount int  `json:"qualityCount"`
	HasWater     bool `json:"hasWater"`
	HasSleep     bool `json:"hasSleep"`
	HasHabits    bool `json:"hasHabits"`
}

type InsightsMind struct {
	Range  Range    `json:"range"`
	Labels []string `json:"labels"`
	// Mood and Energy hold one value per label, 1-5, with 0 for a bucket
	// without a check-in.
	Mood         []int   `json:"mood"`
	Energy       []int   `json:"energy"`
	Journal      Chart   `json:"journal"`
	CheckInCount int     `json:"checkInCount"`
	JournalCount int     `json:"journalCount"`
	AvgMood      float64 `json:"avgMood"`
	AvgEnergy    float64 `json:"avgEnergy"`
	HasCheckIns  bool    `json:"hasCheckIns"`
	HasJournal   bool    `json:"hasJournal"`
}

type GoalProgress struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Progress    int    `json:"progress"`
	HasProgress bool   `json:"hasProgress"`
	Deadline    string `json:"deadline"`
	Overdue     bool   `json:"overdue"`
}

type InsightsProgress struct {
	Range       Range          `json:"range"`
	Goals       []GoalProgress `json:"goals"`
	Notes       Chart          `json:"notes"`
	Statuses    []Segment      `json:"statuses"`
	ActiveCount int            `json:"activeCount"`
	AvgProgress int            `json:"avgProgress"`
	Overdue     int            `json:"overdue"`
	Streak      int            `json:"streak"`
	NoteCount   int            `json:"noteCount"`
	OpenedCount int            `json:"openedCount"`
	HasNotes    bool           `json:"hasNotes"`
}

type TrainingSession struct {
	Name     string    `json:"name"`
	At       time.Time `json:"at"`
	Duration string    `json:"duration"`
	Calories float64   `json:"calories"`
}

type InsightsTraining struct {
	Range        Range             `json:"range"`
	Sessions     []TrainingSession `json:"sessions"`
	Burn         Chart             `json:"burn"`
	Kinds        []Segment         `json:"kinds"`
	Calories     float64           `json:"calories"`
	Delta        Delta             `json:"delta"`
	SessionCount int               `json:"sessionCount"`
	TotalTime    string            `json:"totalTime"`
	HasSessions  bool              `json:"hasSessions"`
	// Plan is absent without a training plan; Recap before the first
	// finished workout in the window.
	Plan  *TrainingPlanWeek    `json:"plan,omitempty"`
	Recap *lifts.LiftRecapView `json:"recap,omitempty"`
	// Lifting is what the window's logged sets add up to; HasSets false
	// before the first set.
	Lifting TrainingLifting `json:"lifting"`
}

// TrainingLifting is the strength section of the training page, worded and
// formatted as the web shows it.
type TrainingLifting struct {
	HasSets  bool   `json:"hasSets"`
	Volume   string `json:"volume"`
	Delta    Delta  `json:"delta"`
	Sets     int    `json:"sets"`
	Workouts int    `json:"workouts"`
	// Weekly is volume per week; HasWeekly false for fewer than two weeks.
	HasWeekly bool                `json:"hasWeekly"`
	Weekly    Chart               `json:"weekly"`
	Exercises []LiftingExercise   `json:"exercises"`
	Records   []LiftingRecord     `json:"records"`
	Muscles   []LiftingMuscleSets `json:"muscles"`
}

type LiftingExercise struct {
	Name   string `json:"name"`
	Sets   int    `json:"sets"`
	Best   string `json:"best"`
	E1RM   string `json:"e1rm"`
	Volume string `json:"volume"`
	// ChangeKg is the estimated max's movement across the window.
	ChangeKg float64 `json:"changeKg"`
}

type LiftingRecord struct {
	Exercise string `json:"exercise"`
	Lift     string `json:"lift"`
	E1RM     string `json:"e1rm"`
	Gain     string `json:"gain"`
	On       string `json:"on"`
}

type LiftingMuscleSets struct {
	Name string `json:"name"`
	Sets int    `json:"sets"`
	// Pct is against the most-trained muscle, 0-100.
	Pct int `json:"pct"`
}

// NutritionEating is the eating-habits section under the nutrition charts:
// tiles, the share of calories by time of day, top foods and habit notes.
type NutritionEating struct {
	HasData bool       `json:"hasData"`
	Tiles   []StatsRow `json:"tiles"`
	Slots   []StatsBar `json:"slots"`
	Foods   []StatsRow `json:"foods"`
	Notes   []StatsRow `json:"notes"`
}

// TrainingPlanWeek is the plan against what was done: this Monday–Sunday
// week day by day, and the requested window as a count.
type TrainingPlanWeek struct {
	Sentence      string            `json:"sentence"`
	Done          int               `json:"done"`
	Planned       int               `json:"planned"`
	Days          []TrainingPlanDay `json:"days"`
	RangeDone     int               `json:"rangeDone"`
	RangePlanned  int               `json:"rangePlanned"`
	RangeSentence string            `json:"rangeSentence"`
}

type TrainingPlanDay struct {
	Weekday string `json:"weekday"`
	Focus   string `json:"focus"`
	// Date is the local calendar day, YYYY-MM-DD.
	Date  string `json:"date"`
	Done  bool   `json:"done"`
	Today bool   `json:"today"`
}

type InsightsNutrition struct {
	Range       Range  `json:"range"`
	AvgCalories string `json:"avgCalories"`
	AvgProtein  string `json:"avgProtein"`
	DaysLogged  int    `json:"daysLogged"`
	Entries     int    `json:"entries"`
	// The goal figures are empty for somebody who has not run the calculator.
	HasGoal      bool      `json:"hasGoal"`
	GoalCalories string    `json:"goalCalories"`
	GoalProtein  string    `json:"goalProtein"`
	Calories     Chart     `json:"calories"`
	Macros       []Segment `json:"macros"`
	HasSplit     bool      `json:"hasSplit"`
	Highlights   []string  `json:"highlights"`
	HasData      bool      `json:"hasData"`
	// Eating is absent from older servers; HasData false without a logged day.
	Eating NutritionEating `json:"eating"`
}

type InsightsCoach struct {
	Range        Range `json:"range"`
	Turns        int   `json:"turns"`
	YourMessages int   `json:"yourMessages"`
	CoachReplies int   `json:"coachReplies"`
	// HelpfulRate is over rated replies alone; an unrated reply is not a
	// thumbs-down.
	HasRatings  bool     `json:"hasRatings"`
	HelpfulRate int      `json:"helpfulRate"`
	Rated       int      `json:"rated"`
	Chart       Chart    `json:"chart"`
	Highlights  []string `json:"highlights"`
	Truncated   bool     `json:"truncated"`
	HasData     bool     `json:"hasData"`
}

type SpendLine struct {
	Label       string `json:"label"`
	Cost        string `json:"cost"`
	Generations int64  `json:"generations"`
	Tokens      int64  `json:"tokens"`
	Pct         int    `json:"pct"`
}

type InsightsSpend struct {
	Range       Range       `json:"range"`
	TotalCost   string      `json:"totalCost"`
	TotalTokens int64       `json:"totalTokens"`
	Generations int64       `json:"generations"`
	Surfaces    []SpendLine `json:"surfaces"`
	Models      []SpendLine `json:"models"`
	HasData     bool        `json:"hasData"`
}

const domainError = "These insights could not be loaded."

// request resolves the reader and window the same way the web handler does.
func request(c *echo.Context) (users.User, timerange.Range) {
	user := auth.MustUser(c.Request().Context())
	return user, timerange.Parse(c.QueryParam("range"), user.Location())
}

func (a *API) timeline(c *echo.Context) error {
	ctx := c.Request().Context()
	user, rg := request(c)
	data, err := a.svc.Timeline(ctx, user, rg, c.QueryParam("kind"))
	if err != nil {
		return httpx.Fail(err, domainError)
	}
	return c.JSON(http.StatusOK, projectTimeline(buildTimelineView(ctx, data)))
}

func (a *API) body(c *echo.Context) error {
	ctx := c.Request().Context()
	user, rg := request(c)
	data, err := a.svc.Body(ctx, user, rg)
	if err != nil {
		return httpx.Fail(err, domainError)
	}
	view, err := buildBodyView(ctx, data)
	if err != nil {
		return httpx.Fail(err, domainError)
	}
	return c.JSON(http.StatusOK, projectBody(view, habitAdherence(data.Habits)))
}

func (a *API) mind(c *echo.Context) error {
	ctx := c.Request().Context()
	user, rg := request(c)
	data, err := a.svc.Mind(ctx, user, rg)
	if err != nil {
		return httpx.Fail(err, domainError)
	}
	view, err := buildMindView(ctx, data)
	if err != nil {
		return httpx.Fail(err, domainError)
	}
	mood, energy := moodEnergyBuckets(data)
	return c.JSON(http.StatusOK, projectMind(view, bucketLabels(ctx, data.Range), mood, energy))
}

func (a *API) progress(c *echo.Context) error {
	ctx := c.Request().Context()
	user, rg := request(c)
	data, err := a.svc.Progress(ctx, user, rg)
	if err != nil {
		return httpx.Fail(err, domainError)
	}
	view, err := buildProgressView(ctx, data)
	if err != nil {
		return httpx.Fail(err, domainError)
	}
	return c.JSON(http.StatusOK, projectProgress(view, goalStatusSegments(ctx, data.Active)))
}

func (a *API) training(c *echo.Context) error {
	ctx := c.Request().Context()
	user, rg := request(c)
	data, err := a.svc.Training(ctx, user, rg)
	if err != nil {
		return httpx.Fail(err, domainError)
	}
	view, err := buildTrainingView(ctx, data)
	if err != nil {
		return httpx.Fail(err, domainError)
	}
	out := projectTraining(view, sessionKindSegments(data.Sessions))
	out.Plan, out.Recap = projectPlanWeek(ctx, data), projectRecap(data)
	return c.JSON(http.StatusOK, out)
}

func (a *API) nutrition(c *echo.Context) error {
	ctx := c.Request().Context()
	user, rg := request(c)
	data, err := a.svc.Nutrition(ctx, user, rg)
	if err != nil {
		return httpx.Fail(err, domainError)
	}
	view, err := buildNutritionView(ctx, data)
	if err != nil {
		return httpx.Fail(err, domainError)
	}
	return c.JSON(http.StatusOK, projectNutrition(view, macroSegments(ctx, data.Days)))
}

func (a *API) coach(c *echo.Context) error {
	ctx := c.Request().Context()
	user, rg := request(c)
	data, err := a.svc.Coach(ctx, user, rg)
	if err != nil {
		return httpx.Fail(err, domainError)
	}
	view, err := buildCoachView(ctx, data)
	if err != nil {
		return httpx.Fail(err, domainError)
	}
	return c.JSON(http.StatusOK, projectCoach(view))
}

func (a *API) spend(c *echo.Context) error {
	ctx := c.Request().Context()
	user, rg := request(c)
	data, err := a.svc.Spend(ctx, user, rg)
	if err != nil {
		return httpx.Fail(err, domainError)
	}
	view, err := buildSpendView(ctx, data)
	if err != nil {
		return httpx.Fail(err, domainError)
	}
	return c.JSON(http.StatusOK, projectSpend(view))
}

func projectTimeline(v insightpages.TimelineView) InsightsTimeline {
	out := InsightsTimeline{
		Range: projectRange(v.Range), Entries: []TimelineEntry{}, Filters: []TimelineFilter{}, Overflow: v.Overflow,
	}
	for _, e := range v.Entries {
		out.Entries = append(out.Entries, TimelineEntry{
			Kind: e.Kind, Label: e.Label, At: e.At, Title: e.Title, Detail: e.Detail, Href: e.Href, Icon: e.Icon,
		})
	}
	for _, c := range v.Chips {
		out.Filters = append(out.Filters, TimelineFilter{Key: c.Key, Label: c.Label, Count: c.Count, Selected: c.Selected})
	}
	return out
}

func projectBody(v insightpages.BodyView, adherence int) InsightsBody {
	out := InsightsBody{
		Range: projectRange(v.Range), Water: projectChart(v.WaterChart.Data), Sleep: projectChart(v.SleepChart.Data),
		Adherence: adherence, Habits: []HabitStat{}, TotalWaterML: v.TotalWaterML, Nights: v.Nights,
		AvgSleepMinutes: v.AvgSleep, AvgQuality: v.AvgQuality, QualityCount: v.QualityCount,
		HasWater: v.HasWater, HasSleep: v.HasSleep, HasHabits: v.HasHabits,
	}
	for _, h := range v.Habits {
		out.Habits = append(out.Habits, HabitStat{Name: h.Name, Kept: h.Kept, Scheduled: h.Scheduled, Streak: h.Streak, Rate: h.Rate})
	}
	return out
}

// projectMind takes mood and energy as numbers because the web draws them from
// a raw chart config that leaves gaps as nulls, which no typed series holds.
func projectMind(v insightpages.MindView, labels []string, mood, energy []int) InsightsMind {
	return InsightsMind{
		Range: projectRange(v.Range), Labels: nonNil(labels), Mood: nonNilInts(mood), Energy: nonNilInts(energy),
		Journal: projectChart(v.JournalChart.Data), CheckInCount: v.CheckInCount, JournalCount: v.JournalCount,
		AvgMood: v.AvgMood, AvgEnergy: v.AvgEnergy, HasCheckIns: v.HasCheckIns, HasJournal: v.HasJournal,
	}
}

func projectProgress(v insightpages.ProgressView, statuses []viz.DonutSegment) InsightsProgress {
	out := InsightsProgress{
		Range: projectRange(v.Range), Goals: []GoalProgress{}, Notes: projectChart(v.NotesChart.Data),
		Statuses: projectSegments(statuses), ActiveCount: v.ActiveCount, AvgProgress: v.AvgProgress,
		Overdue: v.Overdue, Streak: v.Streak, NoteCount: v.NoteCount, OpenedCount: v.OpenedCount, HasNotes: v.HasNotes,
	}
	for _, g := range v.Goals {
		out.Goals = append(out.Goals, GoalProgress{
			ID: g.ID, Title: g.Title, Progress: g.Progress, HasProgress: g.HasProgress, Deadline: g.Deadline, Overdue: g.Overdue,
		})
	}
	return out
}

func projectTraining(v insightpages.TrainingView, kinds []viz.DonutSegment) InsightsTraining {
	out := InsightsTraining{
		Range: projectRange(v.Range), Sessions: []TrainingSession{}, Burn: projectChart(v.BurnChart.Data),
		Kinds: projectSegments(kinds), Calories: v.Calories,
		Delta:        Delta{Direction: v.Delta.Direction, Pct: v.Delta.Pct, HasPrior: v.Delta.HasPrior},
		SessionCount: v.SessionCount, TotalTime: v.TotalTime, HasSessions: v.HasSessions,
	}
	for _, s := range v.Sessions {
		out.Sessions = append(out.Sessions, TrainingSession{Name: s.Name, At: s.At, Duration: s.Duration, Calories: s.Calories})
	}
	out.Lifting = projectLifting(v.Lifting)
	return out
}

func projectLifting(v insightpages.LiftingView) TrainingLifting {
	out := TrainingLifting{
		HasSets: v.HasSets, Volume: v.Volume, Sets: v.Sets, Workouts: v.Workouts,
		Delta:     Delta{Direction: v.Delta.Direction, Pct: v.Delta.Pct, HasPrior: v.Delta.HasPrior},
		HasWeekly: v.HasWeekly, Weekly: projectChart(v.WeeklyChart.Data),
		Exercises: []LiftingExercise{}, Records: []LiftingRecord{}, Muscles: []LiftingMuscleSets{},
	}
	for _, e := range v.Exercises {
		out.Exercises = append(out.Exercises, LiftingExercise{
			Name: e.Name, Sets: e.Sets, Best: e.Best, E1RM: e.E1RM, Volume: e.Volume, ChangeKg: e.Change,
		})
	}
	for _, r := range v.Records {
		out.Records = append(out.Records, LiftingRecord{Exercise: r.Exercise, Lift: r.Lift, E1RM: r.E1RM, Gain: r.Gain, On: r.On})
	}
	for _, m := range v.Muscles {
		out.Muscles = append(out.Muscles, LiftingMuscleSets{Name: m.Name, Sets: m.Sets, Pct: m.Pct})
	}
	return out
}

func projectEating(v insightpages.EatingView) NutritionEating {
	return NutritionEating{
		HasData: v.HasData, Tiles: projectRows(v.Tiles), Slots: projectBars(v.Slots),
		Foods: projectRows(v.Foods), Notes: projectRows(v.Notes),
	}
}

func projectPlanWeek(ctx context.Context, data TrainingData) *TrainingPlanWeek {
	if !data.HasPlan {
		return nil
	}
	today := data.Now.Format("2006-01-02")
	out := &TrainingPlanWeek{
		Sentence: weekSentence(ctx, data.Week), Done: data.Week.Done, Planned: data.Week.Planned,
		Days:      make([]TrainingPlanDay, 0, len(data.Week.Days)),
		RangeDone: data.Adherence.Done, RangePlanned: data.Adherence.Planned, RangeSentence: adherenceSentence(ctx, data.Adherence),
	}
	for _, d := range data.Week.Days {
		date := d.Date.Format("2006-01-02")
		out.Days = append(out.Days, TrainingPlanDay{Weekday: d.Weekday, Focus: d.Focus, Date: date, Done: d.Done, Today: date == today})
	}
	return out
}

func projectRecap(data TrainingData) *lifts.LiftRecapView {
	if !data.HasRecap {
		return nil
	}
	return util.Ptr(lifts.ProjectRecap(data.Recap))
}

func projectNutrition(v insightpages.NutritionView, macros []viz.DonutSegment) InsightsNutrition {
	return InsightsNutrition{
		Range: projectRange(v.Range), AvgCalories: v.AvgCalories, AvgProtein: v.AvgProtein,
		DaysLogged: v.DaysLogged, Entries: v.Entries, HasGoal: v.HasGoal,
		GoalCalories: v.GoalCalories, GoalProtein: v.GoalProtein,
		Calories: projectChart(v.CaloriesChart.Data), Macros: projectSegments(macros), HasSplit: v.HasSplit,
		Highlights: nonNil(v.Highlights), HasData: v.HasData, Eating: projectEating(v.Eating),
	}
}

func projectCoach(v insightpages.CoachView) InsightsCoach {
	return InsightsCoach{
		Range: projectRange(v.Range), Turns: v.Turns, YourMessages: v.YourMessages, CoachReplies: v.CoachReplies,
		HasRatings: v.HasRatings, HelpfulRate: v.HelpfulRate, Rated: v.Rated,
		Chart: projectChart(v.Chart.Data), Highlights: nonNil(v.Highlights), Truncated: v.Truncated, HasData: v.HasData,
	}
}

func projectSpend(v insightpages.SpendView) InsightsSpend {
	return InsightsSpend{
		Range: projectRange(v.Range), TotalCost: v.TotalCost, TotalTokens: v.TotalTokens, Generations: v.Generations,
		Surfaces: projectSpendLines(v.Surfaces), Models: projectSpendLines(v.Models), HasData: v.HasData,
	}
}

func projectSpendLines(rows []insightpages.SpendRow) []SpendLine {
	out := []SpendLine{}
	for _, r := range rows {
		out = append(out, SpendLine{Label: r.Label, Cost: r.Cost, Generations: r.Generations, Tokens: r.Tokens, Pct: r.Pct})
	}
	return out
}

func projectSegments(segments []viz.DonutSegment) []Segment {
	out := []Segment{}
	for _, s := range segments {
		out = append(out, Segment{Label: s.Label, Value: s.Value})
	}
	return out
}

func nonNilInts(s []int) []int {
	if s == nil {
		return []int{}
	}
	return s
}
