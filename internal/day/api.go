package day

import (
	"net/http"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	caffeinecalc "github.com/NorthAIProject/north-client/internal/caffeine/caffeine"
	"github.com/NorthAIProject/north-client/internal/checkins/checkin"
	"github.com/NorthAIProject/north-client/internal/dashboard"
	"github.com/NorthAIProject/north-client/internal/day/day"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/soreness/sore"
	"github.com/NorthAIProject/north-client/internal/supplements/supplement"
)

// API is "My Day" for native clients: one date, every card, and the rules the
// day is read against. Register it behind auth.Bearer.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Register(g *echo.Group) {
	g.GET("/day", a.show)
	g.GET("/day/rules", a.listRules)
	g.GET("/day/trends", a.trends)
	g.GET("/day/checkin", a.checkIn)
	g.PUT("/day/rules/:kind", a.setRule)
	g.DELETE("/day/rules/:kind", a.deleteRule)
}

// DayResponse is one local date.
type DayResponse struct {
	// Date is YYYY-MM-DD in the person's zone.
	Date    string    `json:"date"`
	IsToday bool      `json:"isToday"`
	Now     time.Time `json:"now"`

	Vitals   VitalsView    `json:"vitals"`
	Food     FoodView      `json:"food"`
	Water    DayWaterView  `json:"water"`
	Activity ActivityView  `json:"activity"`
	Sleep    *DaySleepView `json:"sleep,omitempty"`
	Workouts WorkoutsView  `json:"workouts"`
	Body     BodyView      `json:"body"`
	Streak   int           `json:"streak"`
	// Level grows one per five lifetime check-ins and never goes back.
	Level int `json:"level"`

	Caffeine   CaffeineView    `json:"caffeine"`
	Fast       *FastView       `json:"fast,omitempty"`
	Nutrients  NutrientsView   `json:"nutrients"`
	Milestones []MilestoneView `json:"milestones"`

	Timeline []TimelineView `json:"timeline"`
	Markers  []MarkerView   `json:"markers"`

	// Around is the timeline from the evening before the date to the small
	// hours after it, for a rail that shows nearby entries faded beside the
	// date. Absent when only the date itself was read.
	Around []TimelineView `json:"around,omitempty"`
	// Fasts is every fast touching that wider window, for the rail's bands.
	Fasts []FastView `json:"fasts"`
	// CheckIn is the live streak tile: the streak, today's check-in and the
	// version GET /day/checkin is polled with.
	CheckIn StreakTileView `json:"checkIn"`
	// Presets are the quick-add choices, so a client needs no list of its own.
	Presets PresetsView `json:"presets"`
}

// StreakTileView is the streak card's face. Version is opaque: send it back
// as ?v= and an unchanged tile answers 204.
type StreakTileView struct {
	Streak  int               `json:"streak"`
	Version string            `json:"version"`
	Today   *TodayCheckInView `json:"today,omitempty"`
}

// TodayCheckInView is today's check-in, absent until there is one.
type TodayCheckInView struct {
	Mood      int       `json:"mood"`
	Energy    int       `json:"energy"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// PresetsView lists the quick-add choices: caffeine drinks, supplements and
// the regions soreness can be logged against. Keys are open strings.
type PresetsView struct {
	Caffeine        []CaffeinePresetView   `json:"caffeine"`
	Supplements     []SupplementPresetView `json:"supplements"`
	SorenessRegions []string               `json:"sorenessRegions"`
}

type CaffeinePresetView struct {
	Key string `json:"key"`
	MG  int    `json:"mg"`
}

type SupplementPresetView struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// VitalsView holds the strip of small gauges. A null is "not measured".
type VitalsView struct {
	EnergyPercent   *int `json:"energyPercent,omitempty"`
	DaylightMinutes *int `json:"daylightMinutes,omitempty"`
	ScreenMinutes   *int `json:"screenMinutes,omitempty"`
}

type CaffeineView struct {
	TotalMG  int `json:"totalMg"`
	ActiveMG int `json:"activeMg"`
	LimitMG  int `json:"limitMg"`
	// AfterCutoff is true when a drink came after the caffeine cutoff rule.
	AfterCutoff bool `json:"afterCutoff"`
}

type FastView struct {
	StartedAt      time.Time  `json:"startedAt"`
	EndedAt        *time.Time `json:"endedAt,omitempty"`
	TargetHours    int        `json:"targetHours"`
	ElapsedMinutes int        `json:"elapsedMinutes"`
	// Phase is fed, fasting, fat_burning or ketosis.
	Phase    string  `json:"phase"`
	Fraction float64 `json:"fraction"`
}

type NutrientsView struct {
	Covered []string `json:"covered"`
	Missing []string `json:"missing"`
	Total   int      `json:"total"`
}

type MilestoneView struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	MonthsSince int     `json:"monthsSince"`
	Fraction    float64 `json:"fraction"`
	Due         bool    `json:"due"`
}

type BloodPressureView struct {
	Systolic  int       `json:"systolic"`
	Diastolic int       `json:"diastolic"`
	At        time.Time `json:"at"`
}

type SorenessView struct {
	Region string `json:"region"`
	// Severity is 1 (stiff) to 3 (painful).
	Severity int `json:"severity"`
}

type FoodView struct {
	Calories float64    `json:"calories"`
	ProteinG float64    `json:"proteinG"`
	CarbG    float64    `json:"carbG"`
	FatG     float64    `json:"fatG"`
	Goal     *MacroGoal `json:"goal,omitempty"`
}

type MacroGoal struct {
	Calories float64 `json:"calories"`
	ProteinG float64 `json:"proteinG"`
	CarbG    float64 `json:"carbG"`
	FatG     float64 `json:"fatG"`
}

type DayWaterView struct {
	TotalML  int `json:"totalMl"`
	TargetML int `json:"targetMl"`
}

type RingView struct {
	Value   float64 `json:"value"`
	Goal    float64 `json:"goal"`
	Percent int     `json:"percent"`
}

type ActivityView struct {
	Move     RingView `json:"move"`
	Exercise RingView `json:"exercise"`
	Stand    RingView `json:"stand"`
}

type SleepBlockView struct {
	// Stage is deep, rem, core or awake.
	Stage string    `json:"stage"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type DaySleepView struct {
	TotalMinutes int        `json:"totalMinutes"`
	Start        *time.Time `json:"start,omitempty"`
	End          *time.Time `json:"end,omitempty"`
	// Stages is minutes per stage; empty for a manual log.
	Stages  map[string]int   `json:"stages"`
	Blocks  []SleepBlockView `json:"blocks"`
	Quality *int             `json:"quality,omitempty"`
	Source  string           `json:"source"`
}

type WorkoutsView struct {
	Count    int      `json:"count"`
	Minutes  int      `json:"minutes"`
	Calories float64  `json:"calories"`
	Labels   []string `json:"labels"`
}

type BodyView struct {
	WeightKg *float64 `json:"weightKg,omitempty"`
	HeightCm *float64 `json:"heightCm,omitempty"`
	BMI      *float64 `json:"bmi,omitempty"`
	// BMICategory is underweight, healthy, overweight or obese.
	BMICategory    string             `json:"bmiCategory,omitempty"`
	TargetWeightKg *float64           `json:"targetWeightKg,omitempty"`
	ToGoalKg       *float64           `json:"toGoalKg,omitempty"`
	BloodPressure  *BloodPressureView `json:"bloodPressure,omitempty"`
	Soreness       []SorenessView     `json:"soreness"`
}

type TimelineView struct {
	Kind   string    `json:"kind"`
	At     time.Time `json:"at"`
	Title  string    `json:"title"`
	Detail string    `json:"detail,omitempty"`
	Href   string    `json:"href,omitempty"`
	Icon   string    `json:"icon"`
	// PresetKey is the caffeine preset a drink was logged from (espresso,
	// coffee…), so a client can name it in the reader's language. An open
	// string; absent for anything else.
	PresetKey string `json:"presetKey,omitempty"`
}

type MarkerView struct {
	Kind   string    `json:"kind"`
	Label  string    `json:"label"`
	At     time.Time `json:"at"`
	Passed bool      `json:"passed"`
}

type RuleView struct {
	Kind    string `json:"kind"`
	Label   string `json:"label"`
	At      string `json:"at"`
	Enabled bool   `json:"enabled"`
}

type RulesResponse struct {
	Rules []RuleView `json:"rules"`
	// Kinds is every kind a rule may have, so a client can offer the ones not
	// yet set without hardcoding the list.
	Kinds []string `json:"kinds"`
}

type RuleRequest struct {
	At      string `json:"at"`
	Enabled bool   `json:"enabled"`
}

func (a *API) show(c *echo.Context) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	date := ParseDate(c.QueryParam("date"), user.Location(), time.Now())

	snap, err := a.svc.Load(ctx, user, date)
	if err != nil {
		return httpx.Fail(err, "The day could not be loaded.")
	}
	return c.JSON(http.StatusOK, Project(snap))
}

// checkIn is the streak tile alone, for a live poll. An unchanged version
// answers 204, so the poll is one small query.
func (a *API) checkIn(c *echo.Context) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	version, err := a.svc.CheckInVersion(ctx, user.ID)
	if err != nil {
		return httpx.Fail(err, "The check-in could not be loaded.")
	}
	if c.QueryParam("v") == version {
		return c.NoContent(http.StatusNoContent)
	}
	tile, err := a.svc.LoadStreakTile(ctx, user)
	if err != nil {
		return httpx.Fail(err, "The check-in could not be loaded.")
	}
	return c.JSON(http.StatusOK, streakTileJSON(tile.Streak, tile.TodayCheckIn, tile.Version))
}

func streakTileJSON(streak int, today *checkin.CheckIn, version string) StreakTileView {
	out := StreakTileView{Streak: streak, Version: version}
	if today != nil {
		out.Today = &TodayCheckInView{Mood: today.Mood, Energy: today.Energy, UpdatedAt: today.UpdatedAt}
	}
	return out
}

func (a *API) listRules(c *echo.Context) error {
	return a.respondRules(c, http.StatusOK)
}

func (a *API) setRule(c *echo.Context) error {
	var req RuleRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 4 << 10}, "The request body could not be read."); err != nil {
		return err
	}
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	rule := day.Rule{Kind: day.RuleKind(c.Param("kind")), At: req.At, Enabled: req.Enabled}
	if _, err := a.svc.SetRule(ctx, user.ID, rule); err != nil {
		return httpx.Fail(err, "The rule could not be saved.")
	}
	return a.respondRules(c, http.StatusOK)
}

func (a *API) deleteRule(c *echo.Context) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	if err := a.svc.DeleteRule(ctx, user.ID, day.RuleKind(c.Param("kind"))); err != nil {
		return httpx.Fail(err, "The rule could not be removed.")
	}
	return a.respondRules(c, http.StatusOK)
}

func (a *API) respondRules(c *echo.Context, status int) error {
	ctx := c.Request().Context()
	rules, err := a.svc.Rules(ctx, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "The rules could not be loaded.")
	}
	return c.JSON(status, ProjectRules(rules))
}

// ProjectRules is the rules payload.
func ProjectRules(rules []day.Rule) RulesResponse {
	out := RulesResponse{Rules: make([]RuleView, len(rules))}
	for i, rule := range rules {
		out.Rules[i] = RuleView{Kind: string(rule.Kind), Label: rule.Kind.Label(), At: rule.At, Enabled: rule.Enabled}
	}
	for _, k := range day.RuleKinds() {
		out.Kinds = append(out.Kinds, string(k))
	}
	return out
}

// Project turns a snapshot into its JSON shape.
func Project(s Snapshot) DayResponse {
	out := DayResponse{
		Date:    s.Date.Format("2006-01-02"),
		IsToday: s.IsToday,
		Now:     s.Now,
		Vitals:  VitalsView{EnergyPercent: s.EnergyPercent, DaylightMinutes: s.DaylightMinutes, ScreenMinutes: s.ScreenMinutes},
		Level:   s.Level,
		Caffeine: CaffeineView{
			TotalMG: s.Caffeine.TotalMG, ActiveMG: s.Caffeine.ActiveMG, LimitMG: s.Caffeine.LimitMG, AfterCutoff: s.Caffeine.AfterCutoff,
		},
		Nutrients: NutrientsView{
			Covered: append([]string{}, s.Nutrients.Covered...), Missing: append([]string{}, s.Nutrients.Missing...), Total: s.Nutrients.Total(),
		},
		Milestones: make([]MilestoneView, len(s.Milestones)),
		Food: FoodView{
			Calories: s.Food.Calories, ProteinG: s.Food.ProteinG, CarbG: s.Food.CarbG, FatG: s.Food.FatG,
		},
		Water: DayWaterView{TotalML: s.Water.TotalML, TargetML: s.Water.TargetML},
		Activity: ActivityView{
			Move:     ringView(s.Activity.Move),
			Exercise: ringView(s.Activity.Exercise),
			Stand:    ringView(s.Activity.Stand),
		},
		Workouts: WorkoutsView{
			Count: s.Workouts.Count, Minutes: s.Workouts.Minutes, Calories: s.Workouts.Calories,
			Labels: append([]string{}, s.Workouts.Labels...),
		},
		Body: BodyView{
			WeightKg: s.Body.WeightKg, HeightCm: s.Body.HeightCm, BMI: s.Body.BMI,
			BMICategory:    string(s.Body.Category()),
			TargetWeightKg: s.Body.TargetWeightKg,
			Soreness:       make([]SorenessView, len(s.Body.Soreness)),
		},
		Streak:   s.Streak,
		Timeline: make([]TimelineView, len(s.Timeline)),
		Markers:  make([]MarkerView, len(s.Markers)),
		Fasts:    []FastView{},
		CheckIn:  streakTileJSON(s.Streak, s.TodayCheckIn, s.CheckInVersion),
		Presets:  presetsView(),
	}
	for i, m := range s.Milestones {
		out.Milestones[i] = MilestoneView{ID: m.ID, Name: m.Name, MonthsSince: m.MonthsSince, Fraction: m.Fraction, Due: m.Due}
	}
	if s.Fast != nil {
		f := fastView(*s.Fast)
		out.Fast = &f
	}
	if d, ok := s.Body.ToGoal(); ok {
		out.Body.ToGoalKg = &d
	}
	if bp := s.Body.BloodPressure; bp != nil {
		out.Body.BloodPressure = &BloodPressureView{Systolic: bp.Systolic, Diastolic: bp.Diastolic, At: bp.At}
	}
	for i, so := range s.Body.Soreness {
		out.Body.Soreness[i] = SorenessView{Region: so.Region, Severity: so.Severity}
	}
	if s.Food.HasGoal {
		out.Food.Goal = &MacroGoal{
			Calories: s.Food.CalorieGoal, ProteinG: s.Food.ProteinGoalG, CarbG: s.Food.CarbGoalG, FatG: s.Food.FatGoalG,
		}
	}
	if s.Sleep != nil {
		sl := &DaySleepView{
			TotalMinutes: s.Sleep.TotalMinutes,
			Start:        s.Sleep.Start,
			End:          s.Sleep.End,
			Stages:       map[string]int{},
			Blocks:       make([]SleepBlockView, len(s.Sleep.Blocks)),
			Quality:      s.Sleep.Quality,
			Source:       s.Sleep.Source,
		}
		for stage, m := range s.Sleep.StageMinutes {
			sl.Stages[string(stage)] = m
		}
		for i, b := range s.Sleep.Blocks {
			sl.Blocks[i] = SleepBlockView{Stage: string(b.Stage), Start: b.Start, End: b.End}
		}
		out.Sleep = sl
	}
	for i, e := range s.Timeline {
		out.Timeline[i] = timelineView(e)
	}
	if s.Around != nil {
		out.Around = make([]TimelineView, len(s.Around))
		for i, e := range s.Around {
			out.Around[i] = timelineView(e)
		}
	}
	for _, f := range s.Fasts {
		out.Fasts = append(out.Fasts, fastView(f))
	}
	for i, m := range s.Markers {
		out.Markers[i] = MarkerView{Kind: string(m.Kind), Label: m.Kind.Label(), At: m.At, Passed: m.Passed}
	}
	return out
}

func timelineView(e dashboard.Entry) TimelineView {
	v := TimelineView{Kind: string(e.Kind), At: e.At, Title: e.Title, Detail: e.Detail, Href: e.Href, Icon: e.Icon}
	if e.Kind == KindCaffeine {
		if key, ok := caffeinecalc.PresetKeyFor(e.Title); ok {
			v.PresetKey = key
		}
	}
	return v
}

func fastView(f day.Fast) FastView {
	return FastView{
		StartedAt: f.StartedAt, EndedAt: f.EndedAt, TargetHours: f.TargetHours,
		ElapsedMinutes: int(f.Elapsed.Minutes()), Phase: f.Phase, Fraction: f.Fraction,
	}
}

func presetsView() PresetsView {
	out := PresetsView{SorenessRegions: sore.Regions()}
	for _, p := range caffeinecalc.Presets() {
		out.Caffeine = append(out.Caffeine, CaffeinePresetView{Key: p.Key, MG: p.MG})
	}
	for _, p := range supplement.Presets() {
		out.Supplements = append(out.Supplements, SupplementPresetView{Key: p.Key, Name: p.Name})
	}
	return out
}

func ringView(r day.Ring) RingView {
	return RingView{Value: r.Value, Goal: r.Goal, Percent: r.Percent()}
}

type TrendPointView struct {
	At    time.Time `json:"at"`
	Value float64   `json:"value"`
}

type TrendView struct {
	// Key is weight, systolic, active_energy, sleep or caffeine.
	Key    string           `json:"key"`
	Unit   string           `json:"unit"`
	Points []TrendPointView `json:"points"`
	// Headline is the latest reading for a measurement, the daily average
	// for a daily total.
	Headline float64 `json:"headline"`
	Count    int     `json:"count"`
	// WindowDays is how far back the series reaches.
	WindowDays int `json:"windowDays"`
}

type FastBarView struct {
	StartedAt   time.Time `json:"startedAt"`
	Hours       float64   `json:"hours"`
	TargetHours int       `json:"targetHours"`
	Met         bool      `json:"met"`
}

type TrendsResponse struct {
	Series []TrendView   `json:"series"`
	Fasts  []FastBarView `json:"fasts"`
}

func (a *API) trends(c *echo.Context) error {
	ctx := c.Request().Context()
	t, err := a.svc.Trends(ctx, auth.MustUser(ctx))
	if err != nil {
		return httpx.Fail(err, "Trends could not be loaded.")
	}
	return c.JSON(http.StatusOK, ProjectTrends(t))
}

// ProjectTrends is the trends payload. Series with no data are left out.
func ProjectTrends(t Trends) TrendsResponse {
	out := TrendsResponse{Series: []TrendView{}, Fasts: make([]FastBarView, len(t.Fasts))}
	for _, s := range []day.Series{t.Weight, t.Systolic, t.ActiveEnergy, t.Sleep, t.Caffeine} {
		if !s.HasData() {
			continue
		}
		v := TrendView{Key: s.Key, Unit: s.Unit, Headline: s.Headline, Count: s.Count, WindowDays: s.Window, Points: make([]TrendPointView, len(s.Points))}
		for i, p := range s.Points {
			v.Points[i] = TrendPointView{At: p.At, Value: p.Value}
		}
		out.Series = append(out.Series, v)
	}
	for i, f := range t.Fasts {
		out.Fasts[i] = FastBarView{StartedAt: f.StartedAt, Hours: util.RoundHalfUpToScale(f.Hours, 1), TargetHours: f.TargetHours, Met: f.Met()}
	}
	return out
}
