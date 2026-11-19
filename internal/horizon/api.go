package horizon

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/bodymap"
	"github.com/NorthAIProject/north-client/internal/horizon/forecast"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

// API is the forecast for native and web clients. Register behind
// auth.Bearer.
//
// Every label here is a plain string, never a closed enum: a shipped phone
// build must keep decoding the response when a new status, reason or kind is
// added.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Register(g *echo.Group) {
	g.GET("/horizon", a.show)
	g.GET("/horizon/lifts/:key", a.lift)
	g.GET("/horizon/next-sets", a.nextSets)
}

// PointView is the range for one day ahead.
type PointView struct {
	OffsetDays int     `json:"offsetDays"`
	Low        float64 `json:"low"`
	Mid        float64 `json:"mid"`
	High       float64 `json:"high"`
}

// NeedView is one thing missing before a series can be forecast.
type NeedView struct {
	// Code is lift_sessions, lift_recent, weigh_ins or waist_checks.
	Code string `json:"code"`
	Have int    `json:"have"`
	Want int    `json:"want"`
	Text string `json:"text"`
}

// TrackRecordView is how often forecasts at one horizon landed in range.
type TrackRecordView struct {
	HorizonWeeks int `json:"horizonWeeks"`
	Hits         int `json:"hits"`
	Total        int `json:"total"`
	// Kind is "backtest" (what the method would have forecast, recomputed)
	// or "recorded" (forecasts stored at the time). Text is worded for it.
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// SeriesView is one forecast number: where it is and the range ahead.
type SeriesView struct {
	// Status is "ready" or "insufficient".
	Status string `json:"status"`
	// Unit is "kg" or "cm".
	Unit  string     `json:"unit"`
	Needs []NeedView `json:"needs"`
	// Now is the fitted level today. Absent when insufficient.
	Now *float64 `json:"now,omitempty"`
	// Points start at day 0 and may stop before day 84: a forecast never
	// reaches further ahead than the history behind it is long.
	Points []PointView `json:"points"`
	// TrackRecord is absent until enough past forecasts could be scored.
	TrackRecord *TrackRecordView `json:"trackRecord,omitempty"`
}

// LiftView is one lift's estimated one-rep max, forecast.
type LiftView struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Slug string `json:"slug,omitempty"`
	SeriesView
	// Plateau is progressing, holding, plateau, sliding or insufficient.
	Plateau     string `json:"plateau"`
	PlateauText string `json:"plateauText"`
}

// RegionView is one region of the body figure on one day.
type RegionView struct {
	ID string `json:"id"`
	// Fatigue is 0 (fresh) to 1 (spent).
	Fatigue float64 `json:"fatigue"`
	// Retained is the share of strength left after time away, 0.5 to 1.
	Retained float64 `json:"retained"`
}

// FrameView is the body figure on one day ahead.
type FrameView struct {
	OffsetDays int `json:"offsetDays"`
	// Date is the person's calendar date, YYYY-MM-DD.
	Date    string        `json:"date"`
	Shape   bodymap.Shape `json:"shape"`
	Regions []RegionView  `json:"regions"`
}

// ScenarioView is the what-if the response was computed for.
type ScenarioView struct {
	SkipWeeks     int  `json:"skipWeeks"`
	ExtraSessions int  `json:"extraSessions"`
	DeloadNow     bool `json:"deloadNow"`
}

// ReasonView is one sign behind the lighter-week suggestion.
type ReasonView struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

// DeloadView is the suggestion to take a lighter week. Only ever a
// suggestion: nothing changes the plan but the person.
type DeloadView struct {
	Suggested bool `json:"suggested"`
	// SuggestedVolume is "deload" when suggested, absent otherwise: the
	// value PUT /weekly/review takes as volume.
	SuggestedVolume string       `json:"suggestedVolume,omitempty"`
	Reasons         []ReasonView `json:"reasons"`
}

// GoalETAView is when the target weight is expected.
type GoalETAView struct {
	TargetKg float64 `json:"targetKg"`
	// State is reached, window, open or unreachable.
	State string `json:"state"`
	// Earliest and Latest are YYYY-MM-DD; Latest is absent when open.
	Earliest string `json:"earliest,omitempty"`
	Latest   string `json:"latest,omitempty"`
	Text     string `json:"text"`
}

// HorizonView is the whole forecast.
type HorizonView struct {
	// Status is "ready" when at least one series could be forecast, else
	// "insufficient": show Needs and no numbers.
	Status      string       `json:"status"`
	GeneratedAt time.Time    `json:"generatedAt"`
	Scenario    ScenarioView `json:"scenario"`
	Needs       []NeedView   `json:"needs"`
	// Summary is a few sentences for the top of the screen and the coach.
	Summary    []string `json:"summary"`
	Disclaimer string   `json:"disclaimer"`
	// HasPlan is false when no plan is followed: the frames then show
	// training stopping.
	HasPlan    bool         `json:"hasPlan"`
	Frames     []FrameView  `json:"frames"`
	Lifts      []LiftView   `json:"lifts"`
	BodyWeight SeriesView   `json:"bodyWeight"`
	Waist      SeriesView   `json:"waist"`
	GoalETA    *GoalETAView `json:"goalEta,omitempty"`
	Deload     DeloadView   `json:"deload"`
}

func (a *API) show(c *echo.Context) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	q := c.Request().URL.Query()
	sc := forecast.Scenario{
		SkipWeeks:     atoi(q.Get("skipWeeks")),
		ExtraSessions: atoi(q.Get("extraSessions")),
		DeloadNow:     q.Get("deloadNow") == "true",
	}
	view, err := a.svc.View(ctx, user, sc)
	if err != nil {
		return httpx.Fail(err, i18n.T(ctx, "horizon.api.load"))
	}
	return c.JSON(http.StatusOK, view)
}

// View is the forecast as clients read it, worded in ctx's language. The
// coach's tool reads the same view, so both say the same thing.
func (s *Service) View(ctx context.Context, user users.User, sc forecast.Scenario) (HorizonView, error) {
	h, err := s.Forecast(ctx, user, sc)
	if err != nil {
		return HorizonView{}, err
	}
	points, err := s.Points(ctx, user)
	if err != nil {
		return HorizonView{}, err
	}
	records, err := s.Records(ctx, user, h, points)
	if err != nil {
		return HorizonView{}, err
	}
	return project(ctx, h, records), nil
}

func atoi(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

const dateLayout = "2006-01-02"

func project(ctx context.Context, h Horizon, records map[string]Record) HorizonView {
	out := HorizonView{
		Status:      forecast.StatusInsufficient,
		GeneratedAt: h.At.UTC(),
		Scenario:    ScenarioView{SkipWeeks: h.Scenario.SkipWeeks, ExtraSessions: h.Scenario.ExtraSessions, DeloadNow: h.Scenario.DeloadNow},
		Needs:       needs(ctx, h.Needs()),
		Summary:     summary(ctx, h),
		Disclaimer:  i18n.T(ctx, "horizon.disclaimer"),
		HasPlan:     h.HasPlan,
		Frames:      make([]FrameView, 0, len(h.Frames)),
		Lifts:       make([]LiftView, 0, len(h.Lifts)),
		BodyWeight:  series(ctx, h.Weight, "kg", records[SeriesWeight], h.Scenario),
		Waist:       series(ctx, h.Waist, "cm", records[SeriesWaist], h.Scenario),
		Deload:      deload(ctx, h.Deload),
	}
	if h.Ready() {
		out.Status = forecast.StatusReady
	}
	for _, f := range h.Frames {
		frame := FrameView{OffsetDays: f.OffsetDays, Date: f.At.Format(dateLayout), Shape: roundShape(f.Shape), Regions: make([]RegionView, 0, len(f.Regions))}
		for _, r := range f.Regions {
			frame.Regions = append(frame.Regions, RegionView{ID: r.Region, Fatigue: round(r.Fatigue, 2), Retained: round(r.Retained, 2)})
		}
		out.Frames = append(out.Frames, frame)
	}
	for _, l := range h.Lifts {
		out.Lifts = append(out.Lifts, liftView(ctx, l, records[LiftSeries(l.Key)], h.Scenario))
	}
	if h.TargetWeightKg != nil && h.Weight.Status == forecast.StatusReady {
		out.GoalETA = goalETA(ctx, *h.TargetWeightKg, h.WeightETA, h.At)
	}
	return out
}

func liftView(ctx context.Context, l forecast.LiftForecast, record Record, sc forecast.Scenario) LiftView {
	return LiftView{
		Key: l.Key, Name: l.Name, Slug: l.Slug,
		SeriesView:  series(ctx, l.Series, "kg", record, sc),
		Plateau:     l.Plateau,
		PlateauText: i18n.T(ctx, "horizon.plateau."+l.Plateau),
	}
}

func series(ctx context.Context, s forecast.Series, unit string, record Record, sc forecast.Scenario) SeriesView {
	out := SeriesView{Status: s.Status, Unit: unit, Needs: needs(ctx, s.Needs), Points: make([]PointView, 0, len(s.Points))}
	if s.Status != forecast.StatusReady {
		out.Status = forecast.StatusInsufficient
		return out
	}
	now := round(s.Now, 1)
	out.Now = &now
	for _, p := range s.Points {
		out.Points = append(out.Points, PointView{OffsetDays: p.OffsetDays, Low: round(p.Low, 1), Mid: round(p.Mid, 1), High: round(p.High, 1)})
	}
	// A track record is of the plain forecast. Shown beside a what-if it
	// would read as a record of what-ifs, which nobody has.
	if record.Shown() && sc.IsZero() {
		key := "horizon.record.backtest"
		if record.Kind == RecordRecorded {
			key = "horizon.record.recorded"
		}
		out.TrackRecord = &TrackRecordView{
			HorizonWeeks: record.HorizonWeeks, Hits: record.Hits, Total: record.Total,
			Kind: record.Kind, Text: i18n.Tf(ctx, key, record.Hits, record.Total),
		}
	}
	return out
}

func needs(ctx context.Context, in []forecast.Need) []NeedView {
	out := make([]NeedView, 0, len(in))
	for _, n := range in {
		v := NeedView{Code: n.Code, Have: n.Have, Want: n.Want}
		switch {
		case n.Code == forecast.NeedLiftSessions && n.Subject == "":
			v.Text = i18n.Tf(ctx, "horizon.need.lift_sessions.any", n.Have, n.Want)
		case n.Code == forecast.NeedLiftSessions:
			v.Text = i18n.Tf(ctx, "horizon.need.lift_sessions", n.Have, n.Want, n.Subject)
		case n.Code == forecast.NeedLiftRecent:
			v.Text = i18n.Tf(ctx, "horizon.need.lift_recent", n.Subject)
		default:
			v.Text = i18n.Tf(ctx, "horizon.need."+n.Code, n.Have, n.Want)
		}
		out = append(out, v)
	}
	return out
}

func deload(ctx context.Context, d forecast.Deload) DeloadView {
	out := DeloadView{Suggested: d.Suggested, Reasons: make([]ReasonView, 0, len(d.Reasons))}
	if d.Suggested {
		out.SuggestedVolume = "deload"
	}
	for _, code := range d.Reasons {
		out.Reasons = append(out.Reasons, ReasonView{Code: code, Text: i18n.T(ctx, "horizon.deload."+code)})
	}
	return out
}

func goalETA(ctx context.Context, target float64, eta forecast.ETA, now time.Time) *GoalETAView {
	out := &GoalETAView{TargetKg: round(target, 1)}
	day := func(offset int) time.Time { return now.AddDate(0, 0, offset) }
	const layout = "2 January"
	switch {
	case eta.Reached:
		out.State, out.Text = "reached", i18n.T(ctx, "horizon.eta.reached")
	case !eta.Reachable:
		out.State, out.Text = "unreachable", i18n.T(ctx, "horizon.eta.unreachable")
	case eta.Open:
		out.State, out.Earliest = "open", day(eta.EarliestDays).Format(dateLayout)
		out.Text = i18n.Tf(ctx, "horizon.eta.open", i18n.Date(ctx, day(eta.EarliestDays), layout))
	default:
		out.State = "window"
		out.Earliest, out.Latest = day(eta.EarliestDays).Format(dateLayout), day(eta.LatestDays).Format(dateLayout)
		out.Text = i18n.Tf(ctx, "horizon.eta.window", i18n.Date(ctx, day(eta.EarliestDays), layout), i18n.Date(ctx, day(eta.LatestDays), layout))
	}
	return out
}

// summaryWeeks is the horizon the summary speaks about: far enough to mean
// something, near enough that the range is still narrow.
const summaryWeeks = 4

// summary is the forecast in a few sentences: the most-trained ready lift,
// weight, waist, and anything worth acting on.
func summary(ctx context.Context, h Horizon) []string {
	out := []string{}
	num := func(v float64) string { return i18n.Decimal(ctx, v, 1) }
	for _, l := range h.Lifts {
		if r, ok := l.At(summaryWeeks * 7); ok && l.Status == forecast.StatusReady {
			out = append(out, i18n.Tf(ctx, "horizon.summary.lift", l.Name, num(r.Low), num(r.High), summaryWeeks))
			break
		}
	}
	if r, ok := h.Weight.At(summaryWeeks * 7); ok && h.Weight.Status == forecast.StatusReady {
		out = append(out, i18n.Tf(ctx, "horizon.summary.weight", num(r.Low), num(r.High), summaryWeeks))
	}
	if r, ok := h.Waist.At(summaryWeeks * 7); ok && h.Waist.Status == forecast.StatusReady {
		out = append(out, i18n.Tf(ctx, "horizon.summary.waist", num(r.Low), num(r.High), summaryWeeks))
	}
	if h.Deload.Suggested {
		out = append(out, i18n.T(ctx, "horizon.summary.deload"))
	}
	if !h.HasPlan && h.Ready() {
		out = append(out, i18n.T(ctx, "horizon.summary.noplan"))
	}
	return out
}

// HistoryPointView is one training day's best estimated max.
type HistoryPointView struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

// PastForecastView is one four-week forecast the method would have made,
// and what then happened.
type PastForecastView struct {
	// MadeOn is the day it would have been made; it was about 28 days on.
	MadeOn    string  `json:"madeOn"`
	Predicted float64 `json:"predicted"`
	Actual    float64 `json:"actual"`
}

// LiftDetailView is one lift with the history behind its forecast.
type LiftDetailView struct {
	LiftView
	History []HistoryPointView `json:"history"`
	Past    []PastForecastView `json:"past"`
	// FormNote is the newest form check of this lift; absent when there is
	// none.
	FormNote *FormNoteView `json:"formNote,omitempty"`
}

// FormNoteView is what the last form check of a lift said. Every field is
// a plain string: a client decodes it whatever a later version adds.
type FormNoteView struct {
	AnalysisID string `json:"analysisId"`
	Date       string `json:"date"`
	Summary    string `json:"summary"`
	// Confidence is high, medium or low: how clearly the clip showed the lift.
	Confidence string `json:"confidence"`
	Cue        string `json:"cue,omitempty"`
}

func (a *API) lift(c *echo.Context) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	detail, err := a.svc.Lift(ctx, user, c.Param("key"))
	if err != nil {
		return httpx.Fail(err, i18n.T(ctx, "horizon.api.lift"))
	}
	record := Record{detail.Track, RecordBacktest}
	out := LiftDetailView{
		LiftView: liftView(ctx, detail.LiftForecast, record, forecast.Scenario{}),
		History:  make([]HistoryPointView, 0, len(detail.History)),
		Past:     make([]PastForecastView, 0, len(detail.Past)),
	}
	for _, p := range detail.History {
		out.History = append(out.History, HistoryPointView{Date: p.At.In(user.Location()).Format(dateLayout), Value: round(p.Value, 1)})
	}
	for _, m := range detail.Past {
		out.Past = append(out.Past, PastForecastView{MadeOn: m.Origin.Format(dateLayout), Predicted: round(m.Predicted, 1), Actual: round(m.Actual, 1)})
	}
	if n := detail.Form; n != nil {
		out.FormNote = &FormNoteView{
			AnalysisID: n.AnalysisID.String(), Date: n.At.In(user.Location()).Format(dateLayout),
			Summary: n.Summary, Confidence: n.Confidence, Cue: n.Cue,
		}
	}
	return c.JSON(http.StatusOK, out)
}

// NextSetView is the suggested first working set for one lift.
type NextSetView struct {
	Key      string  `json:"key"`
	WeightKg float64 `json:"weightKg"`
	Reps     int     `json:"reps"`
	// Reason is add_load, add_rep, repeat or lighter; ReasonText says it.
	Reason     string `json:"reason"`
	ReasonText string `json:"reasonText"`
	// LastWeightKg and LastReps are the last session's top set.
	LastWeightKg float64 `json:"lastWeightKg"`
	LastReps     int     `json:"lastReps"`
}

type NextSetsView struct {
	// Sets has an entry for each asked-for lift with enough history, in the
	// order asked. A lift without one gets no suggestion.
	Sets []NextSetView `json:"sets"`
}

func (a *API) nextSets(c *echo.Context) error {
	ctx := c.Request().Context()
	var keys []string
	for _, k := range strings.Split(c.Request().URL.Query().Get("keys"), ",") {
		if k = lift.KeyFor("", k); k != "" {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 || len(keys) > MaxNextSetKeys {
		message := i18n.Tf(ctx, "horizon.err.keys", MaxNextSetKeys)
		return httpx.Fail(apperr.FieldErrors{}.Add("keys", message), message)
	}
	found, err := a.svc.NextSets(ctx, auth.MustUser(ctx), keys)
	if err != nil {
		return httpx.Fail(err, i18n.T(ctx, "horizon.api.load"))
	}
	out := NextSetsView{Sets: make([]NextSetView, 0, len(found))}
	seen := map[string]bool{}
	for _, k := range keys {
		n, ok := found[k]
		if !ok || seen[k] {
			continue
		}
		seen[k] = true
		out.Sets = append(out.Sets, NextSetView{
			Key: k, WeightKg: round(n.WeightKg, 2), Reps: n.Reps,
			Reason: n.Reason, ReasonText: i18n.T(ctx, "horizon.next."+n.Reason),
			LastWeightKg: round(n.LastWeightKg, 2), LastReps: n.LastReps,
		})
	}
	return c.JSON(http.StatusOK, out)
}

func round(v float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(v*p) / p
}

func roundShape(s bodymap.Shape) bodymap.Shape {
	return bodymap.Shape{Waist: round(s.Waist, 3), Hips: round(s.Hips, 3), Chest: round(s.Chest, 3), Overall: round(s.Overall, 3)}
}

// SuggestedVolume is the volume worth suggesting for the coming week and, in
// ctx's language, why: "deload" when the lighter-week rule fires, empty
// strings otherwise. For the weekly review, which shows it beside its own
// choice and never applies it.
func (s *Service) SuggestedVolume(ctx context.Context, user users.User) (volume, reason string, err error) {
	h, err := s.Forecast(ctx, user, forecast.Scenario{})
	if err != nil {
		return "", "", err
	}
	view := deload(ctx, h.Deload)
	if !view.Suggested {
		return "", "", nil
	}
	reasons := make([]string, 0, len(view.Reasons))
	for _, r := range view.Reasons {
		reasons = append(reasons, r.Text)
	}
	return view.SuggestedVolume, strings.Join(reasons, ". ") + ".", nil
}
