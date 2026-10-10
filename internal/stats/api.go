package stats

import (
	"net/http"
	"net/url"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/stats/stat"
	"github.com/NorthAIProject/north-client/internal/users"
)

// API is the stats for native clients: the same numbers the insights pages
// show, one area per endpoint, for a ?range= window.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/stats/sleep", a.sleep)
	r.Get("/stats/cardio", a.cardio)
	r.Get("/stats/cardio/kinds/{name}", a.cardioKind)
	r.Get("/stats/eating", a.eating)
	r.Get("/stats/patterns", a.patterns)
}

type StatsDayValue struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

type StatsNight struct {
	Date    string         `json:"date"`
	Minutes int            `json:"minutes"`
	Start   *time.Time     `json:"start,omitempty"`
	End     *time.Time     `json:"end,omitempty"`
	Stages  map[string]int `json:"stages"`
	Quality *int           `json:"quality,omitempty"`
}

type StatsSleep struct {
	Range         string             `json:"range"`
	Nights        []StatsNight       `json:"nights"`
	TargetMinutes int                `json:"targetMinutes"`
	AvgMinutes    int                `json:"avgMinutes"`
	DebtMinutes   int                `json:"debtMinutes"`
	NightsOnTgt   int                `json:"nightsOnTarget"`
	HasTimes      bool               `json:"hasTimes"`
	AvgBedtime    string             `json:"avgBedtime"`
	AvgWake       string             `json:"avgWake"`
	BedtimeSpread int                `json:"bedtimeSpreadMinutes"`
	WakeSpread    int                `json:"wakeSpreadMinutes"`
	WeekdayAvg    int                `json:"weekdayAvgMinutes"`
	WeekendAvg    int                `json:"weekendAvgMinutes"`
	StageShare    map[string]float64 `json:"stageShare"`
}

type StatsKind struct {
	Name       string  `json:"name"`
	Sessions   int     `json:"sessions"`
	Minutes    int     `json:"minutes"`
	DistanceKm float64 `json:"distanceKm"`
	// Measure is "pace" or "speed"; the averages are 0 when not measured.
	Measure  string  `json:"measure,omitempty"`
	AvgPace  float64 `json:"avgPaceSeconds,omitempty"`
	AvgSpeed float64 `json:"avgSpeedKmh,omitempty"`
	AvgHR    float64 `json:"avgHeartRate,omitempty"`
}

// StatsBest is one personal best. Unit says how to read Value: "s" (a
// time), "km", "km/h", "m" or "min".
type StatsBest struct {
	Key   string  `json:"key"`
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	Date  string  `json:"date"`
}

// StatsCardioKind is one activity type over the last year.
type StatsCardioKind struct {
	Name string `json:"name"`
	// Measure is "pace" (seconds per km) or "speed" (km/h): an open string.
	Measure    string  `json:"measure"`
	Sessions   int     `json:"sessions"`
	Minutes    int     `json:"minutes"`
	DistanceKm float64 `json:"distanceKm"`
	ElevationM float64 `json:"elevationM"`
	Indoor     int     `json:"indoor"`
	Outdoor    int     `json:"outdoor"`
	// AvgPace is seconds per km and AvgSpeed km/h; 0 when not measured.
	AvgPace  float64 `json:"avgPaceSeconds"`
	AvgSpeed float64 `json:"avgSpeedKmh"`
	AvgHR    float64 `json:"avgHeartRate"`
	// Monthly is pace or speed (per Measure) by month; Efficiency is metres
	// per heartbeat by month.
	Monthly    []StatsDayValue `json:"monthly"`
	Efficiency []StatsDayValue `json:"efficiency"`
	Bests      []StatsBest     `json:"bests"`
	// UsualDay is a weekday name, empty until there are enough sessions;
	// UsualHour is -1 then.
	UsualDay  string `json:"usualDay"`
	UsualHour int    `json:"usualHour"`
}

type StatsRuns struct {
	Count      int     `json:"count"`
	DistanceKm float64 `json:"distanceKm"`
	// Paces are seconds per kilometre; 0 means no run long enough.
	AvgPace   float64 `json:"avgPaceSeconds"`
	BestPace  float64 `json:"bestPaceSeconds"`
	LongestKm float64 `json:"longestKm"`
	Best5K    float64 `json:"best5kSeconds"`
}

type StatsSession struct {
	Name        string    `json:"name"`
	At          time.Time `json:"at"`
	Minutes     int       `json:"minutes"`
	DistanceKm  float64   `json:"distanceKm"`
	PaceSeconds float64   `json:"paceSeconds"`
	Kcal        float64   `json:"kcal"`
}

type StatsCardio struct {
	Range      string          `json:"range"`
	Sessions   int             `json:"sessions"`
	Minutes    int             `json:"minutes"`
	DistanceKm float64         `json:"distanceKm"`
	Kcal       float64         `json:"kcal"`
	ByKind     []StatsKind     `json:"byKind"`
	WeeklyKm   []StatsDayValue `json:"weeklyKm"`
	WeeklyMins []StatsDayValue `json:"weeklyMinutes"`
	Runs       StatsRuns       `json:"runs"`
	Recent     []StatsSession  `json:"recent"`
	RestingHR  []StatsDayValue `json:"restingHeartRate"`
	HRV        []StatsDayValue `json:"hrv"`
	VO2Max     []StatsDayValue `json:"vo2Max"`
}

type StatsFood struct {
	Label string  `json:"label"`
	Count int     `json:"count"`
	Kcal  float64 `json:"kcal"`
}

type StatsSlot struct {
	Key   string  `json:"key"`
	Kcal  float64 `json:"kcal"`
	Share float64 `json:"share"`
}

type StatsEating struct {
	Range        string          `json:"range"`
	DaysLogged   int             `json:"daysLogged"`
	AvgKcal      float64         `json:"avgKcal"`
	AvgProtein   float64         `json:"avgProteinG"`
	AvgCarb      float64         `json:"avgCarbG"`
	AvgFat       float64         `json:"avgFatG"`
	GoalKcal     float64         `json:"goalKcal"`
	GoalProtein  float64         `json:"goalProteinG"`
	OnTargetDays int             `json:"onTargetDays"`
	ProteinDays  int             `json:"proteinDays"`
	ProteinPerKg float64         `json:"proteinPerKg"`
	TopFoods     []StatsFood     `json:"topFoods"`
	BySlot       []StatsSlot     `json:"bySlot"`
	WeekdayKcal  float64         `json:"weekdayKcal"`
	WeekendKcal  float64         `json:"weekendKcal"`
	LateDays     int             `json:"lateDays"`
	Daily        []StatsDayValue `json:"daily"`
}

type StatsGroup struct {
	Label string  `json:"label"`
	Mean  float64 `json:"mean"`
	Days  int     `json:"days"`
}

type StatsFinding struct {
	Key    string     `json:"key"`
	Title  string     `json:"title"`
	Detail string     `json:"detail"`
	Unit   string     `json:"unit"`
	A      StatsGroup `json:"a"`
	B      StatsGroup `json:"b"`
	Diff   float64    `json:"diff"`
}

type StatsPatterns struct {
	Range    string         `json:"range"`
	Days     int            `json:"days"`
	Findings []StatsFinding `json:"findings"`
}

func (a *API) window(r *http.Request) (users.User, timerange.Range) {
	user := auth.MustUser(r.Context())
	return user, timerange.Parse(r.URL.Query().Get("range"), user.Location())
}

func (a *API) sleep(w http.ResponseWriter, r *http.Request) {
	user, rg := a.window(r)
	st, err := a.svc.Sleep(r.Context(), user, rg)
	if err != nil {
		httpx.Error(w, err, "Sleep stats could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ProjectSleep(rg.Key, st))
}

func (a *API) cardio(w http.ResponseWriter, r *http.Request) {
	user, rg := a.window(r)
	st, err := a.svc.Cardio(r.Context(), user, rg)
	if err != nil {
		httpx.Error(w, err, "Cardio stats could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ProjectCardio(rg.Key, st))
}

func (a *API) cardioKind(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	// Names have spaces ("Indoor running"); a router handed the raw path
	// would leave them escaped.
	name := chi.URLParam(r, "name")
	if unescaped, err := url.PathUnescape(name); err == nil {
		name = unescaped
	}
	st, err := a.svc.CardioKind(r.Context(), user, name, time.Now())
	if err != nil {
		httpx.Error(w, err, "That activity could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ProjectCardioKind(st))
}

// ProjectCardioKind is one activity type as the API returns it.
func ProjectCardioKind(k stat.KindStats) StatsCardioKind {
	out := StatsCardioKind{
		Name: k.Name, Measure: k.Measure, Sessions: k.Sessions, Minutes: k.Seconds / 60,
		DistanceKm: k.DistanceKm, ElevationM: k.ElevationM, Indoor: k.Indoor, Outdoor: k.Outdoor,
		AvgPace: k.AvgPace, AvgSpeed: k.AvgSpeed, AvgHR: k.AvgHR,
		Monthly: days(k.Monthly), Efficiency: days(k.Efficiency), Bests: []StatsBest{},
		UsualDay: k.UsualDay, UsualHour: k.UsualHour,
	}
	for _, b := range k.Bests {
		out.Bests = append(out.Bests, StatsBest{Key: b.Key, Label: b.Label, Value: b.Value, Unit: b.Unit, Date: b.At.Format(time.DateOnly)})
	}
	return out
}

func (a *API) eating(w http.ResponseWriter, r *http.Request) {
	user, rg := a.window(r)
	st, err := a.svc.Eating(r.Context(), user, rg)
	if err != nil {
		httpx.Error(w, err, "Eating stats could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ProjectEating(rg.Key, st))
}

func (a *API) patterns(w http.ResponseWriter, r *http.Request) {
	user, rg := a.window(r)
	found, days, err := a.svc.Patterns(r.Context(), user, rg)
	if err != nil {
		httpx.Error(w, err, "Patterns could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ProjectPatterns(rg.Key, days, found))
}

func days(values []stat.DayValue) []StatsDayValue {
	out := make([]StatsDayValue, len(values))
	for i, v := range values {
		out[i] = StatsDayValue{Date: v.Day.Format(time.DateOnly), Value: v.Value}
	}
	return out
}

func ProjectSleep(key string, st stat.SleepStats) StatsSleep {
	out := StatsSleep{
		Range: key, Nights: []StatsNight{}, TargetMinutes: st.TargetMinutes, AvgMinutes: st.AvgMinutes,
		DebtMinutes: st.DebtMinutes, NightsOnTgt: st.NightsOnTgt, HasTimes: st.HasTimes,
		AvgBedtime: st.AvgBedtime, AvgWake: st.AvgWake, BedtimeSpread: st.BedtimeSpread, WakeSpread: st.WakeSpread,
		WeekdayAvg: st.WeekdayAvg, WeekendAvg: st.WeekendAvg, StageShare: map[string]float64{},
	}
	for k, v := range st.StageShare {
		out.StageShare[k] = v
	}
	for _, n := range st.Nights {
		stages := map[string]int{}
		for k, v := range n.Stages {
			stages[k] = v
		}
		out.Nights = append(out.Nights, StatsNight{
			Date: n.Date.Format(time.DateOnly), Minutes: n.Minutes, Start: n.Start, End: n.End, Stages: stages, Quality: n.Quality,
		})
	}
	return out
}

func ProjectCardio(key string, st CardioStats) StatsCardio {
	out := StatsCardio{
		Range: key, Sessions: st.Sessions, Minutes: st.Seconds / 60, DistanceKm: st.DistanceKm, Kcal: st.Kcal,
		ByKind: []StatsKind{}, WeeklyKm: days(st.WeeklyKm), WeeklyMins: days(st.WeeklyMins),
		Runs: StatsRuns{
			Count: st.Runs.Count, DistanceKm: st.Runs.DistanceKm, AvgPace: st.Runs.AvgPace,
			BestPace: st.Runs.BestPace, LongestKm: st.Runs.LongestKm, Best5K: st.Runs.Best5K,
		},
		Recent: []StatsSession{}, RestingHR: days(st.RestingHR), HRV: days(st.HRV), VO2Max: days(st.VO2Max),
	}
	for _, k := range st.ByKind {
		out.ByKind = append(out.ByKind, StatsKind{
			Name: k.Name, Sessions: k.Sessions, Minutes: k.Seconds / 60, DistanceKm: k.DistanceKm,
			Measure: k.Measure, AvgPace: k.AvgPace, AvgSpeed: k.AvgSpeed, AvgHR: k.AvgHR,
		})
	}
	for _, s := range st.Recent {
		out.Recent = append(out.Recent, StatsSession{
			Name: s.Name, At: s.At, Minutes: s.Seconds / 60, DistanceKm: float64(int(s.DistanceM/100+0.5)) / 10,
			PaceSeconds: float64(int(s.PaceSeconds() + 0.5)), Kcal: float64(int(s.Kcal + 0.5)),
		})
	}
	return out
}

func ProjectEating(key string, st stat.EatingStats) StatsEating {
	out := StatsEating{
		Range: key, DaysLogged: st.DaysLogged, AvgKcal: st.AvgKcal, AvgProtein: st.AvgProtein, AvgCarb: st.AvgCarb,
		AvgFat: st.AvgFat, GoalKcal: st.GoalKcal, GoalProtein: st.GoalProtein, OnTargetDays: st.OnTargetDays,
		ProteinDays: st.ProteinDays, ProteinPerKg: st.ProteinPerKg, TopFoods: []StatsFood{}, BySlot: []StatsSlot{},
		WeekdayKcal: st.WeekdayKcal, WeekendKcal: st.WeekendKcal, LateDays: st.LateDays, Daily: days(st.Daily),
	}
	for _, f := range st.TopFoods {
		out.TopFoods = append(out.TopFoods, StatsFood(f))
	}
	for _, s := range st.BySlot {
		out.BySlot = append(out.BySlot, StatsSlot(s))
	}
	return out
}

func ProjectPatterns(key string, days int, found []stat.Finding) StatsPatterns {
	out := StatsPatterns{Range: key, Days: days, Findings: []StatsFinding{}}
	for _, f := range found {
		out.Findings = append(out.Findings, StatsFinding{
			Key: f.Key, Title: f.Title, Detail: f.Detail, Unit: f.Unit, Diff: f.Diff,
			A: StatsGroup{Label: f.A.Label, Mean: f.A.Mean, Days: f.A.N},
			B: StatsGroup{Label: f.B.Label, Mean: f.B.Mean, Days: f.B.N},
		})
	}
	return out
}
