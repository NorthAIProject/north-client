package workouts

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/exercises"
	"github.com/NorthAIProject/north-client/internal/exercises/exercise"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/users"
)

// API is training for native clients: the same plans, edits and generation as
// the web /training pages.
//
// Every edit is append-only (see applyEdit): it stores a new version and
// answers with it, so the plan id a client holds changes on every edit. An
// edit against a version that is no longer the newest answers 409 with the
// newest one, for the client to show and retry against.
type API struct {
	svc *Service
}

// NewAPI builds the routes; mount them behind auth.RequireBearer.
func NewAPI(svc *Service) *API {
	return &API{svc: svc}
}

func (a *API) Routes(r chi.Router) {
	r.Get("/training/plans", a.listPlans)
	r.Post("/training/plans", a.createPlan)
	r.Get("/training/intake", a.latestIntake)
	r.Get("/training/plans/{planID}", a.showPlan)

	r.Put("/training/plans/{planID}/days/{day}/start-time", a.setStartTime)
	r.Get("/training/plans/{planID}/days/{day}/suggestions", a.suggestForDay)
	r.Post("/training/plans/{planID}/days/{day}/exercises", a.addExercise)
	r.Put("/training/plans/{planID}/days/{day}/exercises/{index}", a.swapExercise)
	r.Delete("/training/plans/{planID}/days/{day}/exercises/{index}", a.removeExercise)
	r.Post("/training/plans/{planID}/days/{day}/exercises/{index}/move", a.moveExercise)
	r.Put("/training/plans/{planID}/days/{day}/exercises/{index}/prescription", a.setPrescription)
	r.Get("/training/plans/{planID}/days/{day}/exercises/{index}/replacements", a.suggestReplacements)
}

// MARK: Shapes

type PlanSummary struct {
	ID         uuid.UUID    `json:"id"`
	Name       string       `json:"name"`
	WeeksTotal int          `json:"weeksTotal"`
	Days       []DaySummary `json:"days"`
	// Source is ai (as generated) or edited.
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"createdAt"`
}

type DaySummary struct {
	Weekday       string `json:"weekday"`
	StartTime     string `json:"startTime,omitempty"`
	Focus         string `json:"focus"`
	ExerciseCount int    `json:"exerciseCount"`
}

type PlanList struct {
	Plans []PlanSummary `json:"plans"`
}

type PlanDetail struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Rationale  string    `json:"rationale"`
	WeeksTotal int       `json:"weeksTotal"`
	Days       []Day     `json:"days"`
	// Problems are where the plan no longer fits the intake it was built
	// from, in words for the person. Reported, never enforced.
	Problems  []string  `json:"problems"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"createdAt"`
}

type Day struct {
	Weekday string `json:"weekday"`
	// StartTime is "HH:MM" in the person's time zone, or absent.
	StartTime string        `json:"startTime,omitempty"`
	Focus     string        `json:"focus"`
	Exercises []DayExercise `json:"exercises"`
}

type DayExercise struct {
	Name        string `json:"name"`
	Sets        int    `json:"sets"`
	Reps        string `json:"reps"`
	RestSeconds int    `json:"restSeconds"`
	Equipment   string `json:"equipment"`
	FormCues    string `json:"formCues,omitempty"`
	Substitute  string `json:"substitute,omitempty"`
	// CatalogSlug links to /exercises/{slug}; empty for a movement the model
	// named that is not in the catalog.
	CatalogSlug string `json:"catalogSlug,omitempty"`
	// HasArt says the catalog entry has pose artwork to draw.
	HasArt    bool     `json:"hasArt"`
	Primary   []string `json:"primaryMuscles"`
	Secondary []string `json:"secondaryMuscles"`
}

type IntakeRequest struct {
	Goal           string `json:"goal"`
	Experience     string `json:"experience"`
	DaysPerWeek    int    `json:"daysPerWeek"`
	SessionMinutes int    `json:"sessionMinutes"`
	// Equipment the person has, from the web intake's list.
	Equipment   []string `json:"equipment"`
	Limitations string   `json:"limitations"`
}

type StartTimeRequest struct {
	// StartTime is "HH:MM", or empty to clear it.
	StartTime string `json:"startTime"`
}

type CatalogChoice struct {
	CatalogSlug string `json:"catalogSlug"`
}

type MoveRequest struct {
	// Direction is up or down.
	Direction string `json:"direction"`
}

type PrescriptionRequest struct {
	Sets        int    `json:"sets"`
	Reps        string `json:"reps"`
	RestSeconds int    `json:"restSeconds"`
}

// MARK: Plans

func (a *API) listPlans(w http.ResponseWriter, r *http.Request) {
	plans, err := a.svc.ListCurrentPlans(r.Context(), auth.MustUser(r.Context()).ID, 20)
	if err != nil {
		httpx.Error(w, err, "Your plans could not be loaded.")
		return
	}
	out := PlanList{Plans: make([]PlanSummary, 0, len(plans))}
	for _, p := range plans {
		out.Plans = append(out.Plans, projectSummary(p))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

// createPlan asks the model for a plan. It can take a while; the web page
// waits on the same call.
func (a *API) createPlan(w http.ResponseWriter, r *http.Request) {
	var req IntakeRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 16 << 10}); err != nil {
		httpx.Error(w, err, "The request body must be a training intake.")
		return
	}
	user := auth.MustUser(r.Context())
	stored, err := a.svc.CreatePlan(r.Context(), user, Intake{
		Goal: strings.TrimSpace(req.Goal), Experience: strings.TrimSpace(req.Experience),
		DaysPerWeek: req.DaysPerWeek, SessionMinutes: req.SessionMinutes,
		Equipment: req.Equipment, Limitations: strings.TrimSpace(req.Limitations),
	})
	if err != nil {
		httpx.Error(w, err, "A plan could not be made. Try again in a moment.")
		return
	}
	a.writePlan(w, r, stored.ID, http.StatusCreated)
}

func (a *API) latestIntake(w http.ResponseWriter, r *http.Request) {
	stored, err := a.svc.LatestIntake(r.Context(), auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "No intake yet.")
		return
	}
	in := stored.Intake
	httpx.WriteJSON(w, http.StatusOK, IntakeRequest{
		Goal: in.Goal, Experience: in.Experience, DaysPerWeek: in.DaysPerWeek, SessionMinutes: in.SessionMinutes,
		Equipment: nonNil(in.Equipment), Limitations: in.Limitations,
	})
}

func (a *API) showPlan(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "planID")
	if !ok {
		return
	}
	a.writePlan(w, r, id, http.StatusOK)
}

func (a *API) writePlan(w http.ResponseWriter, r *http.Request, id uuid.UUID, status int) {
	stored, problems, err := a.svc.PlanForDisplay(r.Context(), id, auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "That plan was not found.")
		return
	}
	httpx.WriteJSON(w, status, projectDetail(stored, problems))
}

// MARK: Edits

func (a *API) setStartTime(w http.ResponseWriter, r *http.Request) {
	var req StartTimeRequest
	a.edit(w, r, false, &req, func(user editUser, t target) (StoredPlan, error) {
		return a.svc.SetStartTime(r.Context(), user, t.plan, t.day, req.StartTime)
	})
}

func (a *API) addExercise(w http.ResponseWriter, r *http.Request) {
	var req CatalogChoice
	a.edit(w, r, false, &req, func(user editUser, t target) (StoredPlan, error) {
		return a.svc.AddExercise(r.Context(), user, t.plan, t.day, req.CatalogSlug)
	})
}

func (a *API) swapExercise(w http.ResponseWriter, r *http.Request) {
	var req CatalogChoice
	a.edit(w, r, true, &req, func(user editUser, t target) (StoredPlan, error) {
		return a.svc.SwapExercise(r.Context(), user, t.plan, t.day, t.index, req.CatalogSlug)
	})
}

func (a *API) removeExercise(w http.ResponseWriter, r *http.Request) {
	a.edit(w, r, true, nil, func(user editUser, t target) (StoredPlan, error) {
		return a.svc.RemoveExercise(r.Context(), user, t.plan, t.day, t.index)
	})
}

func (a *API) moveExercise(w http.ResponseWriter, r *http.Request) {
	var req MoveRequest
	a.edit(w, r, true, &req, func(user editUser, t target) (StoredPlan, error) {
		to := t.index - 1
		if req.Direction == "down" {
			to = t.index + 1
		}
		return a.svc.MoveExercise(r.Context(), user, t.plan, t.day, t.index, to)
	})
}

func (a *API) setPrescription(w http.ResponseWriter, r *http.Request) {
	var req PrescriptionRequest
	a.edit(w, r, true, &req, func(user editUser, t target) (StoredPlan, error) {
		return a.svc.SetPrescription(r.Context(), user, t.plan, t.day, t.index, req.Sets, req.Reps, req.RestSeconds)
	})
}

type (
	editUser = users.User
	target   struct {
		plan       uuid.UUID
		day, index int
	}
)

// edit parses the path and body, runs one edit, and answers with the plan
// version it produced, or with the reason it was refused.
func (a *API) edit(w http.ResponseWriter, r *http.Request, withIndex bool, body any, run func(editUser, target) (StoredPlan, error)) {
	t, ok := parseAPITarget(w, r, withIndex)
	if !ok {
		return
	}
	if body != nil {
		if err := httpx.ReadJSON(w, r, body, httpx.ReadOptions{MaxBytes: 4 << 10}); err != nil {
			httpx.Error(w, err, "The request body could not be read.")
			return
		}
	}
	user := auth.MustUser(r.Context())
	edited, err := run(user, t)
	switch {
	case err == nil:
		a.writePlan(w, r, edited.ID, http.StatusOK)
	case apperr.Is(err, ErrPlanSuperseded):
		// Answer with the newest version, so the client can show what is
		// actually there before trying again.
		current, latestErr := a.svc.CurrentVersionOf(r.Context(), user, t.plan)
		if latestErr != nil {
			httpx.Error(w, latestErr, "That plan was not found.")
			return
		}
		stored, problems, detailErr := a.svc.PlanForDisplay(r.Context(), current.ID, user.ID)
		if detailErr != nil {
			httpx.Error(w, detailErr, "That plan was not found.")
			return
		}
		httpx.WriteJSON(w, http.StatusConflict, projectDetail(stored, problems))
	case apperr.Is(err, apperr.ErrValidation):
		// The plan package's own words: "an exercise needs at least one set".
		message := strings.TrimSuffix(err.Error(), ": "+apperr.ErrValidation.Error())
		httpx.Error(w, apperr.FieldErrors{}.Add("plan", message), message)
	default:
		httpx.Error(w, err, "That change could not be saved.")
	}
}

// MARK: Suggestions

func (a *API) suggestForDay(w http.ResponseWriter, r *http.Request) {
	t, ok := parseAPITarget(w, r, false)
	if !ok {
		return
	}
	found, err := a.svc.SuggestForDay(r.Context(), auth.MustUser(r.Context()), t.plan, t.day)
	writeExercises(w, found, err)
}

func (a *API) suggestReplacements(w http.ResponseWriter, r *http.Request) {
	t, ok := parseAPITarget(w, r, true)
	if !ok {
		return
	}
	found, err := a.svc.SuggestReplacements(r.Context(), auth.MustUser(r.Context()), t.plan, t.day, t.index)
	writeExercises(w, found, err)
}

func writeExercises(w http.ResponseWriter, found []exercise.Exercise, err error) {
	if err != nil {
		httpx.Error(w, err, "Suggestions could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, exercises.ProjectList(found))
}

// MARK: Projection

func projectSummary(p StoredPlan) PlanSummary {
	days := make([]DaySummary, 0, len(p.Plan.Days))
	for _, d := range p.Plan.Days {
		days = append(days, DaySummary{Weekday: d.Weekday, StartTime: d.StartTime, Focus: d.Focus, ExerciseCount: len(d.Exercises)})
	}
	return PlanSummary{ID: p.ID, Name: p.Plan.Name, WeeksTotal: p.Plan.WeeksTotal, Days: days, Source: sourceOf(p), CreatedAt: p.CreatedAt}
}

func projectDetail(p StoredPlan, problems []string) PlanDetail {
	days := make([]Day, 0, len(p.Plan.Days))
	for _, d := range p.Plan.Days {
		exercises := make([]DayExercise, 0, len(d.Exercises))
		for _, e := range d.Exercises {
			exercises = append(exercises, DayExercise{
				Name: e.Name, Sets: e.Sets, Reps: e.Reps, RestSeconds: e.RestSeconds, Equipment: e.Equipment,
				FormCues: e.FormCues, Substitute: e.Substitute, CatalogSlug: e.CatalogSlug, HasArt: e.HasIllustration(),
				Primary: nonNil(e.Primary), Secondary: nonNil(e.Secondary),
			})
		}
		days = append(days, Day{Weekday: d.Weekday, StartTime: d.StartTime, Focus: d.Focus, Exercises: exercises})
	}
	if problems == nil {
		problems = []string{}
	}
	return PlanDetail{
		ID: p.ID, Name: p.Plan.Name, Rationale: p.Plan.Rationale, WeeksTotal: p.Plan.WeeksTotal,
		Days: days, Problems: problems, Source: sourceOf(p), CreatedAt: p.CreatedAt,
	}
}

func sourceOf(p StoredPlan) string {
	if p.Source == "" {
		return SourceAI
	}
	return p.Source
}

func parseAPITarget(w http.ResponseWriter, r *http.Request, withIndex bool) (target, bool) {
	id, ok := pathUUID(w, r, "planID")
	if !ok {
		return target{}, false
	}
	day, err := strconv.Atoi(chi.URLParam(r, "day"))
	if err != nil || day < 0 {
		httpx.Error(w, apperr.ErrNotFound, "No such day.")
		return target{}, false
	}
	t := target{plan: id, day: day, index: -1}
	if withIndex {
		index, err := strconv.Atoi(chi.URLParam(r, "index"))
		if err != nil || index < 0 {
			httpx.Error(w, apperr.ErrNotFound, "No such exercise.")
			return target{}, false
		}
		t.index = index
	}
	return t, true
}

func pathUUID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return uuid.Nil, false
	}
	return id, true
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
