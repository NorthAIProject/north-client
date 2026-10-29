package progresschecks

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/bodymap"
	"github.com/NorthAIProject/north-client/internal/day/day"
	"github.com/NorthAIProject/north-client/internal/lifts"
	"github.com/NorthAIProject/north-client/internal/media"
	"github.com/NorthAIProject/north-client/internal/progresschecks/progresscheck"
	"github.com/NorthAIProject/north-client/internal/quota"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/users"
)

// BodyMaps is what recent training heated. lifts.Service satisfies it.
type BodyMaps interface {
	BodyMap(ctx context.Context, user users.User, days int) (bodymap.Map, error)
}

// bodyMapDays is the heat window /body/current carries, the same as Today's.
const bodyMapDays = 7

// API is progress checks for native clients. Register goes in the JSON
// group; RegisterUploads in the upload group, whose body cap fits photos.
type API struct {
	svc    *Service
	maps   BodyMaps
	quotas *quota.Service
}

func NewAPI(svc *Service, maps BodyMaps, quotas *quota.Service) *API {
	return &API{svc: svc, maps: maps, quotas: quotas}
}

func (a *API) Register(g *echo.Group) {
	g.GET("/progress/checks", a.list)
	g.DELETE("/progress/checks/:checkID", a.delete)
	g.GET("/progress/schedule", a.schedule)
	g.PUT("/progress/schedule", a.setSchedule)
	g.GET("/body/current", a.body)
	g.GET("/progress/assist", a.assist)
	g.POST("/progress/compare", a.compare, a.quotas.GuardAPI(quota.ProgressEstimate))
}

func (a *API) RegisterUploads(g *echo.Group) {
	g.POST("/progress/checks", a.create)
	g.POST("/progress/checks/estimate", a.estimate, a.quotas.GuardAPI(quota.ProgressEstimate))
}

// maxCheckUpload fits three photos at media's cap plus the framing.
const maxCheckUpload = 3*media.MaxImageBytes + 1<<20

// ProgressCheckRequest is the "check" part of the multipart upload. Every
// number is optional; measuredOn defaults to today. Cadence, when present,
// also sets the reminder cadence.
type ProgressCheckRequest struct {
	MeasuredOn       string   `json:"measuredOn,omitempty"`
	Cadence          string   `json:"cadence,omitempty"`
	WeightKg         *float64 `json:"weightKg,omitempty"`
	WaistCm          *float64 `json:"waistCm,omitempty"`
	HipsCm           *float64 `json:"hipsCm,omitempty"`
	ChestCm          *float64 `json:"chestCm,omitempty"`
	ThighCm          *float64 `json:"thighCm,omitempty"`
	ArmCm            *float64 `json:"armCm,omitempty"`
	Source           string   `json:"source,omitempty"`
	AssistConfidence string   `json:"assistConfidence,omitempty"`
	AssistNotes      string   `json:"assistNotes,omitempty"`
}

// ScheduleRequest sets the cadence; an empty cadence turns the reminder off.
type ScheduleRequest struct {
	Cadence string `json:"cadence"`
}

// ProgressPhotoView is one pose. URL is signed and lasts two hours; MediaID
// is the stored photo, for a client that links it through its own media route
// (the web's /app/media/{id}), which signs a fresh URL on every open.
type ProgressPhotoView struct {
	Pose    string    `json:"pose"`
	URL     string    `json:"url"`
	MediaID uuid.UUID `json:"mediaId"`
}

type ProgressCheckView struct {
	ID            uuid.UUID `json:"id"`
	MeasuredOn    string    `json:"measuredOn"`
	WeightKg      *float64  `json:"weightKg,omitempty"`
	WeightCarried bool      `json:"weightCarried"`
	WaistCm       *float64  `json:"waistCm,omitempty"`
	HipsCm        *float64  `json:"hipsCm,omitempty"`
	ChestCm       *float64  `json:"chestCm,omitempty"`
	ThighCm       *float64  `json:"thighCm,omitempty"`
	ArmCm         *float64  `json:"armCm,omitempty"`
	// Source is manual or assisted, as an open string.
	Source           string              `json:"source"`
	AssistConfidence *string             `json:"assistConfidence,omitempty"`
	AssistNotes      string              `json:"assistNotes,omitempty"`
	Complete         bool                `json:"complete"`
	Photos           []ProgressPhotoView `json:"photos"`
	CreatedAt        time.Time           `json:"createdAt"`
}

// ProgressScheduleView is the reminder. Cadence and NextDueOn are absent when
// no cadence is set.
type ProgressScheduleView struct {
	Cadence   string `json:"cadence,omitempty"`
	NextDueOn string `json:"nextDueOn,omitempty"`
	Due       bool   `json:"due"`
}

type ProgressChecksView struct {
	Checks   []ProgressCheckView  `json:"checks"`
	Schedule ProgressScheduleView `json:"schedule"`
	// Comparison is the latest first-versus-latest photo comparison, absent
	// when there is none.
	Comparison *ProgressComparisonView `json:"comparison,omitempty"`
	// CanCompare is two checks with a front or side photo to compare.
	CanCompare bool `json:"canCompare"`
}

// ProgressComparisonView is a model's observation of what visibly changed
// between the first and the latest check with photos. Show it labelled as an
// observation, not a measurement.
type ProgressComparisonView struct {
	FromCheckID uuid.UUID `json:"fromCheckId"`
	ToCheckID   uuid.UUID `json:"toCheckId"`
	FromOn      string    `json:"fromOn"`
	ToOn        string    `json:"toOn"`
	Summary     string    `json:"summary"`
	// Confidence is low, medium or high, as an open string.
	Confidence string `json:"confidence"`
	Limits     string `json:"limits,omitempty"`
	// ProviderLabel is who read the photos.
	ProviderLabel string `json:"providerLabel"`
	// Stale is a newer check with photos since; comparing again covers it.
	Stale     bool      `json:"stale"`
	CreatedAt time.Time `json:"createdAt"`
}

// BodyMorphView is the figure's shape, each target 0..1.
type BodyMorphView = bodymap.Shape

// BodyCurrentView is everything the figure is drawn from. The shape comes
// from the latest progress check; muscles are the same heat as /body/map.
type BodyCurrentView struct {
	SourceCheckID *uuid.UUID            `json:"sourceCheckId,omitempty"`
	MeasuredOn    string                `json:"measuredOn,omitempty"`
	WeightKg      *float64              `json:"weightKg,omitempty"`
	BMI           *float64              `json:"bmi,omitempty"`
	BMIBand       string                `json:"bmiBand,omitempty"`
	Morph         BodyMorphView         `json:"morph"`
	Muscles       []lifts.BodyMapMuscle `json:"muscles"`
}

type ProgressCheckCreated struct {
	Check ProgressCheckView `json:"check"`
	Body  BodyCurrentView   `json:"body"`
}

func (a *API) list(c *echo.Context) error {
	limit := DefaultLimit
	if raw := c.QueryParam("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > MaxLimit {
			return httpx.Fail(apperr.FieldErrors{}.Add("limit", "Use a limit from 1 to 52."), "Use a limit from 1 to 52.")
		}
		limit = n
	}
	return a.respondList(c, http.StatusOK, limit)
}

func (a *API) create(c *echo.Context) error {
	r := c.Request()
	user := auth.MustUser(r.Context())
	r.Body = http.MaxBytesReader(c.Response(), r.Body, maxCheckUpload)
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		return httpx.Fail(apperr.FieldErrors{}.Add("front", "The photos could not be read."), "The photos could not be read.")
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	var req ProgressCheckRequest
	if raw := r.FormValue("check"); raw != "" {
		dec := json.NewDecoder(strings.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			return httpx.Fail(apperr.Wrap(apperr.ErrValidation, "decode check: %v", err), "The check could not be read.")
		}
	}
	in, cadence, err := req.input()
	if err != nil {
		return httpx.Fail(err, "The check could not be read.")
	}

	uploads, closeAll, err := formPhotos(r)
	defer closeAll()
	if err != nil {
		return httpx.Fail(err, "The photos could not be read.")
	}

	check, err := a.svc.Save(r.Context(), user, in, uploads)
	if err != nil {
		return httpx.Fail(err, "The check could not be saved.")
	}
	if cadence != "" {
		if _, err = a.svc.SetCadence(r.Context(), user, cadence); err != nil {
			return httpx.Fail(err, "The check was saved, but the reminder could not be set.")
		}
	}

	view, err := a.project(r.Context(), user, check)
	if err != nil {
		return httpx.Fail(err, "The check was saved, but could not be loaded.")
	}
	body, err := a.bodyView(r.Context(), user)
	if err != nil {
		return httpx.Fail(err, "The check was saved, but the figure could not be loaded.")
	}
	return c.JSON(http.StatusCreated, ProgressCheckCreated{Check: view, Body: body})
}

func (req ProgressCheckRequest) input() (Input, progresscheck.Cadence, error) {
	in := Input{
		Measurements: progresscheck.Measurements{
			WeightKg: req.WeightKg, WaistCm: req.WaistCm, HipsCm: req.HipsCm,
			ChestCm: req.ChestCm, ThighCm: req.ThighCm, ArmCm: req.ArmCm,
		},
		Source: req.Source, AssistConfidence: req.AssistConfidence, AssistNotes: req.AssistNotes,
	}
	if req.MeasuredOn != "" {
		on, err := time.Parse(time.DateOnly, req.MeasuredOn)
		if err != nil {
			return Input{}, "", apperr.FieldErrors{}.Add("measuredOn", "The date is YYYY-MM-DD.")
		}
		in.MeasuredOn = on
	}
	var cadence progresscheck.Cadence
	if req.Cadence != "" {
		c, ok := progresscheck.ParseCadence(req.Cadence)
		if !ok {
			return Input{}, "", apperr.FieldErrors{}.Add("cadence", "Cadence is weekly, biweekly or monthly.")
		}
		cadence = c
	}
	return in, cadence, nil
}

// formPhotos opens each pose's file field that is present. The returned func
// closes them all, and is safe to call when nothing opened.
func formPhotos(r *http.Request) ([]PhotoUpload, func(), error) {
	var uploads []PhotoUpload
	var closers []func() error
	closeAll := func() {
		for _, c := range closers {
			_ = c()
		}
	}
	for _, pose := range progresscheck.Poses {
		file, header, err := r.FormFile(string(pose))
		if err == http.ErrMissingFile {
			continue
		}
		if err != nil {
			return nil, closeAll, apperr.FieldErrors{}.Add(string(pose), "That photo could not be read.")
		}
		closers = append(closers, file.Close)
		uploads = append(uploads, PhotoUpload{Pose: pose, Filename: header.Filename, Size: header.Size, Body: file})
	}
	return uploads, closeAll, nil
}

func (a *API) delete(c *echo.Context) error {
	ctx := c.Request().Context()
	id, err := uuid.Parse(c.Param("checkID"))
	if err != nil {
		return httpx.Fail(apperr.ErrNotFound, "No such progress check.")
	}
	if err := a.svc.Delete(ctx, auth.MustUser(ctx), id); err != nil {
		return httpx.Fail(err, "The check could not be deleted.")
	}
	return a.respondList(c, http.StatusOK, DefaultLimit)
}

func (a *API) schedule(c *echo.Context) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	sched, due, err := a.svc.Due(ctx, user, time.Now())
	if err != nil {
		return httpx.Fail(err, "The reminder could not be loaded.")
	}
	return c.JSON(http.StatusOK, ProjectSchedule(sched, due))
}

func (a *API) setSchedule(c *echo.Context) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	var req ScheduleRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 1 << 10}, "The request body could not be read."); err != nil {
		return err
	}
	if strings.TrimSpace(req.Cadence) == "" {
		if err := a.svc.StopSchedule(ctx, user); err != nil {
			return httpx.Fail(err, "The reminder could not be turned off.")
		}
	} else {
		cadence, ok := progresscheck.ParseCadence(req.Cadence)
		if !ok {
			return httpx.Fail(apperr.FieldErrors{}.Add("cadence", "Cadence is weekly, biweekly or monthly."), "Cadence is weekly, biweekly or monthly.")
		}
		if _, err := a.svc.SetCadence(ctx, user, cadence); err != nil {
			return httpx.Fail(err, "The reminder could not be set.")
		}
	}
	return a.schedule(c)
}

func (a *API) body(c *echo.Context) error {
	ctx := c.Request().Context()
	view, err := a.bodyView(ctx, auth.MustUser(ctx))
	if err != nil {
		return httpx.Fail(err, "The body could not be loaded.")
	}
	return c.JSON(http.StatusOK, view)
}

func (a *API) respondList(c *echo.Context, status, limit int) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	checks, err := a.svc.List(ctx, user, limit)
	if err != nil {
		return httpx.Fail(err, "Your progress checks could not be loaded.")
	}
	sched, due, err := a.svc.Due(ctx, user, time.Now())
	if err != nil {
		return httpx.Fail(err, "The reminder could not be loaded.")
	}
	out := ProgressChecksView{Checks: make([]ProgressCheckView, len(checks)), Schedule: ProjectSchedule(sched, due)}
	cmp, stale, ok, err := a.svc.SavedComparison(ctx, user)
	if err != nil {
		return httpx.Fail(err, "Your progress checks could not be loaded.")
	}
	if ok {
		view := ProjectComparison(cmp, stale)
		out.Comparison = &view
	}
	if out.CanCompare, err = a.svc.CanCompare(ctx, user); err != nil {
		return httpx.Fail(err, "Your progress checks could not be loaded.")
	}
	for i, check := range checks {
		if out.Checks[i], err = a.project(ctx, user, check); err != nil {
			return httpx.Fail(err, "Your progress checks could not be loaded.")
		}
	}
	return c.JSON(status, out)
}

func (a *API) project(ctx context.Context, user users.User, c Check) (ProgressCheckView, error) {
	urls := make(map[uuid.UUID]string, len(c.Photos))
	for _, p := range c.Photos {
		u, err := a.svc.PhotoURL(ctx, user, p.MediaID)
		if err != nil {
			return ProgressCheckView{}, err
		}
		urls[p.MediaID] = u
	}
	return ProjectCheck(c, urls), nil
}

func (a *API) bodyView(ctx context.Context, user users.User) (BodyCurrentView, error) {
	body, err := a.svc.Body(ctx, user)
	if err != nil {
		return BodyCurrentView{}, err
	}
	var heat lifts.BodyMapView
	if a.maps != nil {
		m, err := a.maps.BodyMap(ctx, user, bodyMapDays)
		if err != nil {
			return BodyCurrentView{}, err
		}
		heat = lifts.NewBodyMapView(m, user.Location())
	}
	return ProjectBody(body, heat.Muscles), nil
}

// ProjectCheck is one check's payload; urls maps each photo's media id to its
// signed link.
func ProjectCheck(c Check, urls map[uuid.UUID]string) ProgressCheckView {
	out := ProgressCheckView{
		ID: c.ID, MeasuredOn: c.MeasuredOn.Format(time.DateOnly),
		WeightKg: c.WeightKg, WeightCarried: c.WeightCarried,
		WaistCm: c.WaistCm, HipsCm: c.HipsCm, ChestCm: c.ChestCm, ThighCm: c.ThighCm, ArmCm: c.ArmCm,
		Source: c.Source, AssistConfidence: c.AssistConfidence, AssistNotes: c.AssistNotes,
		Complete: c.Complete(), Photos: make([]ProgressPhotoView, len(c.Photos)), CreatedAt: c.CreatedAt,
	}
	for i, p := range c.Photos {
		out.Photos[i] = ProgressPhotoView{Pose: string(p.Pose), URL: urls[p.MediaID], MediaID: p.MediaID}
	}
	return out
}

func ProjectSchedule(s Schedule, due bool) ProgressScheduleView {
	if s.Cadence == "" {
		return ProgressScheduleView{}
	}
	return ProgressScheduleView{Cadence: string(s.Cadence), NextDueOn: s.NextDueOn.Format(time.DateOnly), Due: due}
}

// ProjectBody is the /body/current payload.
func ProjectBody(b Body, muscles []lifts.BodyMapMuscle) BodyCurrentView {
	out := BodyCurrentView{WeightKg: b.WeightKg, BMI: b.BMI, Morph: b.Shape, Muscles: muscles}
	if out.Muscles == nil {
		out.Muscles = []lifts.BodyMapMuscle{}
	}
	if b.Check != nil {
		out.SourceCheckID = &b.Check.ID
		out.MeasuredOn = b.Check.MeasuredOn.Format(time.DateOnly)
	}
	if b.BMI != nil {
		out.BMIBand = string(day.CategoryFor(*b.BMI))
	}
	return out
}

// ProgressAssistView says whether photos can be estimated for this person,
// and by whom, so the client can name the provider before sending anything.
type ProgressAssistView struct {
	Available bool `json:"available"`
	// ProviderLabel is the provider as a person knows it, e.g. "OpenAI".
	ProviderLabel string `json:"providerLabel,omitempty"`
	// OwnProvider is the person's own connected key, billed to them.
	OwnProvider bool `json:"ownProvider"`
}

// ProgressEstimateView is a model's reading of the photos. Nothing is saved:
// the client fills its form, and a save sends source "assisted" with the
// confidence and notes. Absent measurements were not visible.
type ProgressEstimateView struct {
	WaistCm *float64 `json:"waistCm,omitempty"`
	HipsCm  *float64 `json:"hipsCm,omitempty"`
	ChestCm *float64 `json:"chestCm,omitempty"`
	// Confidence is low, medium or high, as an open string.
	Confidence string `json:"confidence"`
	Notes      string `json:"notes"`
}

type ProgressEstimateResult struct {
	Estimate      ProgressEstimateView `json:"estimate"`
	ProviderLabel string               `json:"providerLabel"`
	OwnProvider   bool                 `json:"ownProvider"`
}

func (a *API) assist(c *echo.Context) error {
	ctx := c.Request().Context()
	dest, err := a.svc.AssistDestination(ctx, auth.MustUser(ctx))
	if errors.Is(err, ErrAssistUnavailable) {
		return c.JSON(http.StatusOK, ProgressAssistView{})
	}
	if err != nil {
		return httpx.Fail(err, "Photo estimates could not be checked.")
	}
	return c.JSON(http.StatusOK, ProgressAssistView{Available: true, ProviderLabel: dest.Label, OwnProvider: dest.Own})
}

// estimate reads measurements off the photos in a multipart body with the
// same pose parts as create. It stores nothing.
func (a *API) estimate(c *echo.Context) error {
	r := c.Request()
	user := auth.MustUser(r.Context())
	r.Body = http.MaxBytesReader(c.Response(), r.Body, maxCheckUpload)
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		return httpx.Fail(apperr.FieldErrors{}.Add("front", "The photos could not be read."), "The photos could not be read.")
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()

	uploads, closeAll, err := formPhotos(r)
	defer closeAll()
	if err != nil {
		return httpx.Fail(err, "The photos could not be read.")
	}
	est, dest, err := a.svc.Estimate(r.Context(), user, uploads)
	if err != nil {
		return httpx.Fail(err, estimateFailure(err))
	}
	return c.JSON(http.StatusOK, ProgressEstimateResult{
		Estimate:      ProjectEstimate(est),
		ProviderLabel: dest.Label,
		OwnProvider:   dest.Own,
	})
}

// estimateFailure is the sentence for a refused estimate. Field errors carry
// their own; the rest say what the person can still do.
func estimateFailure(err error) string {
	if errors.Is(err, ErrAssistUnavailable) {
		return "Photo estimates aren't available. Enter your measurements by hand."
	}
	return "The photos couldn't be estimated right now. Enter your measurements by hand, or try again."
}

func ProjectEstimate(e Estimate) ProgressEstimateView {
	return ProgressEstimateView{
		WaistCm: e.WaistCm, HipsCm: e.HipsCm, ChestCm: e.ChestCm,
		Confidence: e.Confidence, Notes: e.Notes,
	}
}

// compare describes what changed between the first and latest checks with
// photos, stores it, and answers with it.
func (a *API) compare(c *echo.Context) error {
	ctx := c.Request().Context()
	cmp, err := a.svc.Compare(ctx, auth.MustUser(ctx))
	if err != nil {
		return httpx.Fail(err, compareFailure(err))
	}
	return c.JSON(http.StatusOK, ProjectComparison(cmp, false))
}

func compareFailure(err error) string {
	var fe apperr.FieldErrors
	switch {
	case apperr.As(err, &fe):
		return "Comparing needs two checks with a front or side photo."
	case errors.Is(err, ErrAssistUnavailable):
		return "Photo comparisons aren't available right now."
	default:
		return "The photos couldn't be compared right now. Try again later."
	}
}

func ProjectComparison(c Comparison, stale bool) ProgressComparisonView {
	return ProgressComparisonView{
		FromCheckID: c.FromCheckID, ToCheckID: c.ToCheckID,
		FromOn: c.FromOn.Format(time.DateOnly), ToOn: c.ToOn.Format(time.DateOnly),
		Summary: c.Summary, Confidence: c.Confidence, Limits: c.Limits,
		ProviderLabel: c.Provider, Stale: stale, CreatedAt: c.CreatedAt,
	}
}
