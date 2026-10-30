package fitness

import (
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/fitness/fitnessview"
	"github.com/NorthAIProject/north-client/internal/fitness/strava"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// The web fitness pages as JSON: the hub, the Strava activities page and its
// session list, and the browser half of the Strava connection. The web app
// renders these; the numbers are worked out here, by the same code the Go
// pages use, so the two cannot disagree.

// WithHub adds the hub's service, which /fitness/hub reads. Without it the
// route answers 404.
func (a *API) WithHub(svc *Service) *API {
	a.hub = svc
	return a
}

// Hub is the service behind the hub page, for wiring the API.
func (h *Handler) Hub() *Service { return h.svc }

// dateOnly is how a calendar day crosses the wire: the person's local day,
// with no zone to misread it in.
const dateOnly = "2006-01-02"

type HubWeekDay struct {
	Date    string  `json:"date"`
	Minutes float64 `json:"minutes"`
	IsToday bool    `json:"isToday"`
}

type HubWeek struct {
	Days       []HubWeekDay `json:"days"`
	Sessions   int          `json:"sessions"`
	Minutes    float64      `json:"minutes"`
	DistanceKm float64      `json:"distanceKm"`
	Calories   float64      `json:"calories"`
}

type HubSession struct {
	ActivityCode    string    `json:"activityCode"`
	Name            string    `json:"name"`
	Category        string    `json:"category"`
	Source          string    `json:"source"`
	EndedAt         time.Time `json:"endedAt"`
	DurationSeconds int       `json:"durationSeconds"`
	DistanceKm      float64   `json:"distanceKm"`
	Calories        float64   `json:"calories"`
}

type HubDayValue struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

type HubWeight struct {
	Kg      float64   `json:"kg"`
	DeltaKg *float64  `json:"deltaKg,omitempty"`
	At      time.Time `json:"at"`
}

type HubStrengthRow struct {
	Name     string  `json:"name"`
	BestKg   float64 `json:"bestKg"`
	E1RMKg   float64 `json:"e1rmKg"`
	ChangeKg float64 `json:"changeKg"`
}

type HubStrengthRecord struct {
	Exercise string  `json:"exercise"`
	WeightKg float64 `json:"weightKg"`
	Reps     int     `json:"reps"`
	E1RMKg   float64 `json:"e1rmKg"`
	On       string  `json:"on"`
}

type HubStrength struct {
	Workouts  int                `json:"workouts"`
	Sets      int                `json:"sets"`
	VolumeKg  float64            `json:"volumeKg"`
	Exercises []HubStrengthRow   `json:"exercises"`
	Record    *HubStrengthRecord `json:"record,omitempty"`
}

type HubCardio struct {
	Sessions        int     `json:"sessions"`
	Minutes         int     `json:"minutes"`
	DistanceKm      float64 `json:"distanceKm"`
	Runs            int     `json:"runs"`
	BestPaceSeconds float64 `json:"bestPaceSeconds"`
	Best5KSeconds   float64 `json:"best5kSeconds"`
	LongestKm       float64 `json:"longestKm"`
	RestingHR       float64 `json:"restingHr"`
}

type HubPlanDay struct {
	Weekday       string `json:"weekday"`
	StartTime     string `json:"startTime,omitempty"`
	Focus         string `json:"focus,omitempty"`
	ExerciseCount int    `json:"exerciseCount"`
}

type HubMacros struct {
	Calories float64 `json:"calories"`
	ProteinG float64 `json:"proteinG"`
	CarbG    float64 `json:"carbG"`
	FatG     float64 `json:"fatG"`
}

type HubMeals struct {
	Logged HubMacros `json:"logged"`
	Goal   HubMacros `json:"goal"`
}

// FitnessHub is the /app/fitness page. Optional cards are absent (or, for the
// device series, empty) when the page leaves them out.
type FitnessHub struct {
	Strava StravaStatus `json:"strava"`
	Week   HubWeek      `json:"week"`
	Recent []HubSession `json:"recent"`

	// Steps and VO2 are a device's daily series, oldest first; empty when no
	// device has pushed one.
	Steps []HubDayValue `json:"steps"`
	VO2   []HubDayValue `json:"vo2"`

	Weight   *HubWeight   `json:"weight,omitempty"`
	Strength *HubStrength `json:"strength,omitempty"`
	Cardio   *HubCardio   `json:"cardio,omitempty"`

	PlanID      *uuid.UUID  `json:"planId,omitempty"`
	NextSession *HubPlanDay `json:"nextSession,omitempty"`
	DoneToday   *HubPlanDay `json:"doneToday,omitempty"`

	Meals *HubMeals `json:"meals,omitempty"`

	// DeviceReadings are the last week from a wearable, already sentences.
	DeviceReadings []string `json:"deviceReadings"`
}

func (a *API) hubView(c *echo.Context) error {
	if a.hub == nil {
		return httpx.Fail(apperr.ErrNotFound, "Not found.")
	}
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	snap, err := a.hub.Load(ctx, user)
	if err != nil {
		return httpx.Fail(err, "The fitness hub could not be loaded.")
	}
	loc := user.Location()
	return c.JSON(http.StatusOK, projectHub(snap, buildHubData(snap, loc, time.Now(), ""), loc))
}

func projectHub(snap Snapshot, data fitnessview.HubData, loc *time.Location) FitnessHub {
	out := FitnessHub{
		Strava:         statusView(snap.StravaStatus),
		Recent:         make([]HubSession, 0, len(snap.Recent)),
		Steps:          trendDays(data.Steps),
		VO2:            trendDays(data.VO2),
		DeviceReadings: append([]string{}, snap.DeviceReadings...),
	}

	week := HubWeek{
		Days: make([]HubWeekDay, 0, len(snap.Week.Days)), Sessions: snap.Week.Sessions,
		Minutes: snap.Week.Minutes, DistanceKm: snap.Week.DistanceKm, Calories: snap.Calories7d,
	}
	days := trailingDays(loc, len(snap.Week.Days))
	for i, d := range snap.Week.Days {
		week.Days = append(week.Days, HubWeekDay{Date: days[i].Format(dateOnly), Minutes: d.Minutes, IsToday: d.IsToday})
	}
	out.Week = week

	for _, s := range snap.Recent {
		out.Recent = append(out.Recent, HubSession{
			ActivityCode: s.ActivityCode, Name: s.Name, Category: s.Category, Source: s.Source, EndedAt: s.EndedAt,
			DurationSeconds: int(s.Duration.Seconds()), DistanceKm: s.DistanceKm, Calories: s.Calories,
		})
	}

	if data.Weight != nil {
		out.Weight = &HubWeight{Kg: data.Weight.Kg, At: data.Weight.At}
		if data.Weight.HasDelta {
			delta := data.Weight.DeltaKg
			out.Weight.DeltaKg = &delta
		}
	}
	if st := data.Strength; st != nil {
		out.Strength = &HubStrength{Workouts: st.Workouts, Sets: st.Sets, VolumeKg: st.VolumeKg, Exercises: []HubStrengthRow{}}
		for _, e := range st.Exercises {
			out.Strength.Exercises = append(out.Strength.Exercises, HubStrengthRow{Name: e.Name, BestKg: e.BestKg, E1RMKg: e.E1RMKg, ChangeKg: e.ChangeKg})
		}
		if rec := st.Record; rec != nil {
			out.Strength.Record = &HubStrengthRecord{
				Exercise: rec.Exercise, WeightKg: rec.WeightKg, Reps: rec.Reps, E1RMKg: rec.E1RMKg, On: rec.On.Format(dateOnly),
			}
		}
	}
	if c := data.Cardio; c != nil {
		out.Cardio = &HubCardio{
			Sessions: c.Sessions, Minutes: c.Minutes, DistanceKm: c.DistanceKm, Runs: c.Runs,
			BestPaceSeconds: c.BestPaceSeconds, Best5KSeconds: c.Best5KSeconds, LongestKm: c.LongestKm, RestingHR: c.RestingHR,
		}
	}

	if snap.PlanID != uuid.Nil {
		id := snap.PlanID
		out.PlanID = &id
	}
	if d := snap.NextSession; d != nil {
		out.NextSession = &HubPlanDay{Weekday: d.Weekday, StartTime: d.StartTime, Focus: d.Focus, ExerciseCount: len(d.Exercises)}
	}
	if d := snap.DoneToday; d != nil {
		out.DoneToday = &HubPlanDay{Weekday: d.Weekday, StartTime: d.StartTime, Focus: d.Focus, ExerciseCount: len(d.Exercises)}
	}
	if p := snap.MealProgress; snap.HasMealProgress && p != nil {
		out.Meals = &HubMeals{
			Logged: HubMacros{Calories: p.Logged.Calories, ProteinG: p.Logged.ProteinG, CarbG: p.Logged.CarbG, FatG: p.Logged.FatG},
			Goal:   HubMacros{Calories: p.Goal.Calories, ProteinG: p.Goal.ProteinG, CarbG: p.Goal.CarbG, FatG: p.Goal.FatG},
		}
	}
	return out
}

// trendDays is a device card's series as the page draws it: trailing empty
// days already trimmed (trendView), empty when there is no card.
func trendDays(t *fitnessview.Trend) []HubDayValue {
	if t == nil {
		return []HubDayValue{}
	}
	out := make([]HubDayValue, len(t.Days))
	for i, d := range t.Days {
		out[i] = HubDayValue{Date: d.Day.Format(dateOnly), Value: d.Value}
	}
	return out
}

func statusView(s strava.Status) StravaStatus {
	return StravaStatus{
		Configured: s.Configured, Connected: s.Connected, Unavailable: s.Unavailable,
		LastSyncedAt: s.LastSyncedAt, LastSyncAttemptedAt: s.LastSyncAttemptedAt,
		LastSyncError: s.LastSyncError, SyncPending: s.SyncPending,
	}
}

// ---------------------------------------------------------------------------
// The activities page.

// TrendBandView is one sport family's minutes per day, rounded as the chart
// draws them.
type TrendBandView struct {
	// Family is run, ride, swim, walk, strength or other; an open set.
	Family string    `json:"family"`
	Values []float64 `json:"values"`
}

type TrendDayView struct {
	Date    string  `json:"date"`
	Minutes float64 `json:"minutes"`
	Future  bool    `json:"future"`
}

// TrainingTrend is the activities page's chart and the figures its sentence
// is built from.
type TrainingTrend struct {
	Days []TrendDayView `json:"days"`
	// Bands walk the families in legend order, one value per day.
	Bands  []TrendBandView `json:"bands"`
	Normal []float64       `json:"normal"`

	RecentMinutes float64 `json:"recentMinutes"`
	NormalMinutes float64 `json:"normalMinutes"`
	Ratio         float64 `json:"ratio"`
	StreakWeeks   int     `json:"streakWeeks"`
	// Shape is no_history, too_new, well_down, easier, steady, building or
	// big_week; an open set.
	Shape string `json:"shape"`

	Sessions    int     `json:"sessions"`
	DistanceM   float64 `json:"distanceM"`
	ElevationM  float64 `json:"elevationM"`
	ActiveDays  int     `json:"activeDays"`
	CountedDays int     `json:"countedDays"`
}

type StravaSessionView struct {
	StravaID    int64      `json:"stravaId,omitempty"`
	Name        string     `json:"name"`
	Sport       string     `json:"sport"`
	Family      string     `json:"family"`
	DistanceM   float64    `json:"distanceM"`
	ElevationM  float64    `json:"elevationM"`
	MovingTimeS int        `json:"movingTimeS"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
}

type StravaSessionPage struct {
	Sessions   []StravaSessionView `json:"sessions"`
	Page       int                 `json:"page"`
	PerPage    int                 `json:"perPage"`
	TotalPages int                 `json:"totalPages"`
	Total      int                 `json:"total"`
}

// FitnessActivities is the /app/fitness/activities page. Trend and Sessions
// are present only when Strava is connected and could be read.
type FitnessActivities struct {
	Strava   StravaStatus       `json:"strava"`
	Trend    *TrainingTrend     `json:"trend,omitempty"`
	Sessions *StravaSessionPage `json:"sessions,omitempty"`
}

// activities mirrors Handler.activities: a read failure marks the status
// unavailable rather than failing the page.
func (a *API) activities(c *echo.Context) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	log := middleware.FromContext(ctx)

	status, err := a.strava.Status(ctx, user.ID)
	if err != nil {
		log.Error("read strava status", slog.Any("error", err))
		status = strava.Status{Configured: a.strava.Configured(), Unavailable: true}
	}

	out := FitnessActivities{}
	if status.Connected {
		trend, err := a.strava.Trend(ctx, user.ID, user.Location(), 0)
		if err != nil {
			log.Error("build training trend", slog.Any("error", err))
			status.Unavailable = true
		} else {
			view := trainingTrendView(trend)
			out.Trend = &view
		}
		page, err := a.strava.Sessions(ctx, user.ID, user.Location(), sessionPageParam(c.Request()), 0)
		if err != nil {
			log.Error("read activity sessions", slog.Any("error", err))
			status.Unavailable = true
		} else {
			view := sessionPageView(page)
			out.Sessions = &view
		}
		if status.Unavailable {
			out.Trend, out.Sessions = nil, nil
		}
	}
	out.Strava = statusView(status)
	c.Response().Header().Set("Cache-Control", "private, no-store")
	return c.JSON(http.StatusOK, out)
}

// activitySessions is one page of the list, for the pager.
func (a *API) activitySessions(c *echo.Context) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	page, err := a.strava.Sessions(ctx, user.ID, user.Location(), sessionPageParam(c.Request()), 0)
	if err != nil {
		return httpx.Fail(err, "Your activities could not be read just now.")
	}
	c.Response().Header().Set("Cache-Control", "private, no-store")
	return c.JSON(http.StatusOK, sessionPageView(page))
}

func trainingTrendView(t strava.Trend) TrainingTrend {
	out := TrainingTrend{
		Days: make([]TrendDayView, len(t.Days)), Bands: make([]TrendBandView, 0, len(strava.Families)),
		Normal: roundTenths(t.Normal), RecentMinutes: t.RecentMinutes, NormalMinutes: t.NormalMinutes, Ratio: t.Ratio,
		StreakWeeks: t.StreakWeeks, Shape: shapeName(t.Shape), Sessions: t.Sessions, DistanceM: t.DistanceM,
		ElevationM: t.ElevationM, ActiveDays: t.ActiveDays, CountedDays: t.CountedDays,
	}
	if out.Normal == nil {
		out.Normal = []float64{}
	}
	for i, d := range t.Days {
		out.Days[i] = TrendDayView{Date: d.Date.Format(dateOnly), Minutes: d.Minutes, Future: d.Future}
	}
	// The same sums trendChartOption draws.
	for _, family := range strava.Families {
		values := make([]float64, len(t.Days))
		for i, day := range t.Days {
			for _, s := range day.Sessions {
				if s.Family == family {
					values[i] += s.Minutes()
				}
			}
		}
		out.Bands = append(out.Bands, TrendBandView{Family: string(family), Values: roundTenths(values)})
	}
	return out
}

// roundTenths rounds each value to one decimal, as the Go chart does.
func roundTenths(in []float64) []float64 {
	out := make([]float64, len(in))
	for i, v := range in {
		out[i] = util.RoundHalfUpToScale(v, 1)
	}
	return out
}

func shapeName(s strava.TrendShape) string {
	switch s {
	case strava.ShapeNoHistory:
		return "no_history"
	case strava.ShapeTooNew:
		return "too_new"
	case strava.ShapeWellDown:
		return "well_down"
	case strava.ShapeEasier:
		return "easier"
	case strava.ShapeSteady:
		return "steady"
	case strava.ShapeBuilding:
		return "building"
	case strava.ShapeBigWeek:
		return "big_week"
	default:
		return "well_down"
	}
}

func sessionPageView(p strava.SessionPage) StravaSessionPage {
	out := StravaSessionPage{
		Sessions: make([]StravaSessionView, len(p.Sessions)),
		Page:     p.Page, PerPage: p.PerPage, TotalPages: p.TotalPages, Total: p.Total,
	}
	for i, s := range p.Sessions {
		v := StravaSessionView{
			StravaID: s.StravaID, Name: s.Name, Sport: s.Sport, Family: string(s.Family),
			DistanceM: s.DistanceM, ElevationM: s.ElevationM, MovingTimeS: s.MovingTimeS,
		}
		if !s.StartedAt.IsZero() {
			started := s.StartedAt
			v.StartedAt = &started
		}
		out.Sessions[i] = v
	}
	return out
}

// ---------------------------------------------------------------------------
// The browser half of connecting Strava.
//
// The web app owns the redirect and the callback page; the client secret and
// the code exchange stay here. The state lives server-side, bound to the
// person who asked, so the callback needs no cookie of its own.

type StravaWebConnectRequest struct {
	// CallbackURL is the web app's /app/fitness/strava/callback, absolute.
	CallbackURL string `json:"callbackUrl"`
}

type StravaWebFinishRequest struct {
	State string `json:"state"`
	Code  string `json:"code"`
}

type StravaConnectResult struct {
	// Result is connected, expired, unverified or failed; an open set.
	Result string `json:"result"`
}

func (a *API) connectWeb(c *echo.Context) error {
	var req StravaWebConnectRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 4 << 10}, "The request body could not be read."); err != nil {
		return err
	}
	if !validWebCallback(req.CallbackURL) {
		msg := "The callback must be the web app's " + strava.WebCallbackPath + "."
		return httpx.Fail(apperr.FieldErrors{}.Add("callbackUrl", msg), msg)
	}
	ctx := c.Request().Context()
	consent, err := a.strava.BeginWebConnect(ctx, auth.MustUser(ctx).ID, req.CallbackURL)
	if err != nil {
		return httpx.Fail(err, "Strava is not available right now.")
	}
	return c.JSON(http.StatusOK, StravaConnect{AuthorizeURL: consent})
}

func (a *API) finishWeb(c *echo.Context) error {
	var req StravaWebFinishRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 4 << 10}, "The request body could not be read."); err != nil {
		return err
	}
	ctx := c.Request().Context()
	result := "connected"
	err := a.strava.FinishWebConnect(ctx, auth.MustUser(ctx).ID, req.State, req.Code)
	switch {
	case err == nil:
	case apperr.Is(err, apperr.ErrNotFound):
		result = "expired"
	case apperr.Is(err, apperr.ErrForbidden):
		result = "unverified"
	default:
		middleware.FromContext(ctx).Error("web strava connect failed", slog.Any("error", err))
		result = "failed"
	}
	return c.JSON(http.StatusOK, StravaConnectResult{Result: result})
}

// validWebCallback accepts only an absolute http(s) URL whose path is the web
// callback, so the endpoint cannot be used to mint consent URLs that return
// anywhere else on the domain.
func validWebCallback(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil {
		return false
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return false
	}
	return u.Path == strava.WebCallbackPath && u.RawQuery == "" && u.Fragment == ""
}
