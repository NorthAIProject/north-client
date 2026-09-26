package day

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/day/day"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is "My Day" for native clients: one date, every card, and the rules the
// day is read against. Mount behind auth.RequireBearer.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/day", a.show)
	r.Get("/day/rules", a.listRules)
	r.Put("/day/rules/{kind}", a.setRule)
	r.Delete("/day/rules/{kind}", a.deleteRule)
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

	Timeline []TimelineView `json:"timeline"`
	Markers  []MarkerView   `json:"markers"`
}

// VitalsView holds the strip of small gauges. A null is "not measured".
type VitalsView struct {
	EnergyPercent   *int `json:"energyPercent,omitempty"`
	DaylightMinutes *int `json:"daylightMinutes,omitempty"`
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
	BMICategory string `json:"bmiCategory,omitempty"`
}

type TimelineView struct {
	Kind   string    `json:"kind"`
	At     time.Time `json:"at"`
	Title  string    `json:"title"`
	Detail string    `json:"detail,omitempty"`
	Href   string    `json:"href,omitempty"`
	Icon   string    `json:"icon"`
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

func (a *API) show(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	date := ParseDate(r.URL.Query().Get("date"), user.Location(), time.Now())

	snap, err := a.svc.Load(r.Context(), user, date)
	if err != nil {
		httpx.Error(w, err, "The day could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, Project(snap))
}

func (a *API) listRules(w http.ResponseWriter, r *http.Request) {
	a.respondRules(w, r, http.StatusOK)
}

func (a *API) setRule(w http.ResponseWriter, r *http.Request) {
	var req RuleRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 4 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return
	}
	user := auth.MustUser(r.Context())
	rule := day.Rule{Kind: day.RuleKind(chi.URLParam(r, "kind")), At: req.At, Enabled: req.Enabled}
	if _, err := a.svc.SetRule(r.Context(), user.ID, rule); err != nil {
		httpx.Error(w, err, "The rule could not be saved.")
		return
	}
	a.respondRules(w, r, http.StatusOK)
}

func (a *API) deleteRule(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	if err := a.svc.DeleteRule(r.Context(), user.ID, day.RuleKind(chi.URLParam(r, "kind"))); err != nil {
		httpx.Error(w, err, "The rule could not be removed.")
		return
	}
	a.respondRules(w, r, http.StatusOK)
}

func (a *API) respondRules(w http.ResponseWriter, r *http.Request, status int) {
	rules, err := a.svc.Rules(r.Context(), auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "The rules could not be loaded.")
		return
	}
	httpx.WriteJSON(w, status, ProjectRules(rules))
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
		Vitals:  VitalsView{EnergyPercent: s.EnergyPercent, DaylightMinutes: s.DaylightMinutes},
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
			BMICategory: string(s.Body.Category()),
		},
		Streak:   s.Streak,
		Timeline: make([]TimelineView, len(s.Timeline)),
		Markers:  make([]MarkerView, len(s.Markers)),
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
		out.Timeline[i] = TimelineView{Kind: string(e.Kind), At: e.At, Title: e.Title, Detail: e.Detail, Href: e.Href, Icon: e.Icon}
	}
	for i, m := range s.Markers {
		out.Markers[i] = MarkerView{Kind: string(m.Kind), Label: m.Kind.Label(), At: m.At, Passed: m.Passed}
	}
	return out
}

func ringView(r day.Ring) RingView {
	return RingView{Value: r.Value, Goal: r.Goal, Percent: r.Percent()}
}
