package workouts

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/exercises"
	"github.com/NorthAIProject/north-client/internal/exercises/exercise"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
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
	svc    *Service
	recaps Recaps
	loads  Loads
}

// NewAPI builds the routes; register them behind auth.Bearer.
func NewAPI(svc *Service) *API {
	return &API{svc: svc}
}

func (a *API) Register(g *echo.Group) {
	g.GET("/training/plans", a.listPlans)
	g.POST("/training/plans", a.createPlan)
	g.GET("/training/intake", a.latestIntake)
	g.GET("/training/overview", a.overview)
	g.GET("/training/plans/:planID", a.showPlan)
	g.POST("/training/plans/:planID/activate", a.activatePlan)

	g.GET("/training/week", a.showWeek)
	g.PUT("/training/week", a.setWeek)
	g.DELETE("/training/week", a.resetWeek)
	g.POST("/training/week/suggestion", a.suggestWeek)

	g.PUT("/training/plans/:planID/days/:day/start-time", a.setStartTime)
	g.GET("/training/plans/:planID/days/:day/suggestions", a.suggestForDay)
	g.POST("/training/plans/:planID/days/:day/exercises", a.addExercise)
	g.PUT("/training/plans/:planID/days/:day/exercises/:index", a.swapExercise)
	g.DELETE("/training/plans/:planID/days/:day/exercises/:index", a.removeExercise)
	g.POST("/training/plans/:planID/days/:day/exercises/:index/move", a.moveExercise)
	g.PUT("/training/plans/:planID/days/:day/exercises/:index/prescription", a.setPrescription)
	g.GET("/training/plans/:planID/days/:day/exercises/:index/replacements", a.suggestReplacements)
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
	// Active is the plan being followed; the list puts it first.
	Active bool `json:"active"`
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
	// WeekVolume is this week's choice from the weekly review: hold, build or
	// deload. Each exercise's thisWeekSets already reflects it.
	WeekVolume string `json:"weekVolume"`
	// Active is the plan being followed.
	Active bool `json:"active"`
}

type Day struct {
	Weekday string `json:"weekday"`
	// StartTime is "HH:MM" in the person's time zone, or absent.
	StartTime string        `json:"startTime,omitempty"`
	Focus     string        `json:"focus"`
	Exercises []DayExercise `json:"exercises"`
	// CompletedThisWeek is true once this day was trained in the current
	// Monday–Sunday week, on whichever day the week scheduled it.
	CompletedThisWeek bool `json:"completedThisWeek"`
	// IsNext marks the one day to train next: the session of the week's first
	// unfinished day from today on. See GET /training/week for which day.
	IsNext bool `json:"isNext"`
	// Lighter marks today when the person chose a lighter session this
	// morning; its exercises' thisWeekSets are already reduced.
	Lighter bool `json:"lighter,omitempty"`
}

type DayExercise struct {
	Name string `json:"name"`
	// Sets is the plan as written, which is what an edit changes.
	Sets int `json:"sets"`
	// ThisWeekSets is what this week asks for after the weekly review's
	// volume; the same as sets in a hold week.
	ThisWeekSets int    `json:"thisWeekSets"`
	Reps         string `json:"reps"`
	// Load is the weight as an imported plan wrote it; empty otherwise.
	Load        string `json:"load,omitempty"`
	RestSeconds int    `json:"restSeconds"`
	Equipment   string `json:"equipment"`
	FormCues    string `json:"formCues,omitempty"`
	Substitute  string `json:"substitute,omitempty"`
	// CatalogSlug links to /exercises/{slug}; empty for a movement the model
	// named that is not in the catalog.
	CatalogSlug string `json:"catalogSlug,omitempty"`
	// HasArt says the catalog entry has pose artwork to draw.
	HasArt bool `json:"hasArt"`
	// IllustrationSlug names the artwork's frames (/assets/exercises/{slug});
	// absent when HasArt is false.
	IllustrationSlug string   `json:"illustrationSlug,omitempty"`
	Primary          []string `json:"primaryMuscles"`
	Secondary        []string `json:"secondaryMuscles"`
	Stabilizers      []string `json:"stabilizerMuscles"`
}

type IntakeRequest struct {
	Goal           string `json:"goal"`
	Experience     string `json:"experience"`
	DaysPerWeek    int    `json:"daysPerWeek"`
	SessionMinutes int    `json:"sessionMinutes"`
	// Equipment the person has, from the web intake's list.
	Equipment   []string `json:"equipment"`
	Limitations string   `json:"limitations"`
	// Weekdays are the days they can train, "Monday".."Sunday", as many as
	// daysPerWeek; empty or absent lets the plan pick.
	Weekdays []string `json:"weekdays,omitempty"`
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

func (a *API) listPlans(c *echo.Context) error {
	ctx := c.Request().Context()
	plans, err := a.svc.ListCurrentPlans(ctx, auth.MustUser(ctx).ID, 20)
	if err != nil {
		return httpx.Fail(err, "Your plans could not be loaded.")
	}
	out := PlanList{Plans: make([]PlanSummary, 0, len(plans))}
	for i, p := range plans {
		summary := projectSummary(p)
		// ListCurrentPlans puts the followed plan first.
		summary.Active = i == 0
		out.Plans = append(out.Plans, summary)
	}
	return c.JSON(http.StatusOK, out)
}

// createPlan asks the model for a plan. It can take a while; the web page
// waits on the same call.
func (a *API) createPlan(c *echo.Context) error {
	var req IntakeRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 16 << 10}, "The request body must be a training intake."); err != nil {
		return err
	}
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	stored, err := a.svc.CreatePlan(ctx, user, Intake{
		Goal: strings.TrimSpace(req.Goal), Experience: strings.TrimSpace(req.Experience),
		DaysPerWeek: req.DaysPerWeek, SessionMinutes: req.SessionMinutes,
		Equipment: req.Equipment, Limitations: strings.TrimSpace(req.Limitations),
		Weekdays: req.Weekdays,
	})
	if err != nil {
		return httpx.Fail(err, "A plan could not be made. Try again in a moment.")
	}
	return a.writePlan(c, stored.ID, http.StatusCreated)
}

func (a *API) latestIntake(c *echo.Context) error {
	ctx := c.Request().Context()
	stored, err := a.svc.LatestIntake(ctx, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "No intake yet.")
	}
	in := stored.Intake
	return c.JSON(http.StatusOK, IntakeRequest{
		Goal: in.Goal, Experience: in.Experience, DaysPerWeek: in.DaysPerWeek, SessionMinutes: in.SessionMinutes,
		Equipment: nonNil(in.Equipment), Limitations: in.Limitations, Weekdays: in.Weekdays,
	})
}

func (a *API) showPlan(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "planID")
	if err != nil {
		return err
	}
	return a.writePlan(c, id, http.StatusOK)
}

func (a *API) writePlan(c *echo.Context, id uuid.UUID, status int) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	stored, problems, err := a.svc.PlanForDisplay(ctx, id, user.ID)
	if err != nil {
		return httpx.Fail(err, "That plan was not found.")
	}
	progress, err := a.svc.WeekProgress(ctx, user, time.Now())
	if err != nil {
		return httpx.Fail(err, "That plan could not be loaded.")
	}
	active, err := a.svc.ActivePlan(ctx, user.ID)
	if err != nil {
		return httpx.Fail(err, "That plan could not be loaded.")
	}
	return c.JSON(status, projectDetail(stored, problems, progress, active.IntakeID))
}

// MARK: Edits

func (a *API) setStartTime(c *echo.Context) error {
	var req StartTimeRequest
	return a.edit(c, false, &req, func(user editUser, t target) (StoredPlan, error) {
		return a.svc.SetStartTime(c.Request().Context(), user, t.plan, t.day, req.StartTime)
	})
}

func (a *API) addExercise(c *echo.Context) error {
	var req CatalogChoice
	return a.edit(c, false, &req, func(user editUser, t target) (StoredPlan, error) {
		return a.svc.AddExercise(c.Request().Context(), user, t.plan, t.day, req.CatalogSlug)
	})
}

func (a *API) swapExercise(c *echo.Context) error {
	var req CatalogChoice
	return a.edit(c, true, &req, func(user editUser, t target) (StoredPlan, error) {
		return a.svc.SwapExercise(c.Request().Context(), user, t.plan, t.day, t.index, req.CatalogSlug)
	})
}

func (a *API) removeExercise(c *echo.Context) error {
	return a.edit(c, true, nil, func(user editUser, t target) (StoredPlan, error) {
		return a.svc.RemoveExercise(c.Request().Context(), user, t.plan, t.day, t.index)
	})
}

func (a *API) moveExercise(c *echo.Context) error {
	var req MoveRequest
	return a.edit(c, true, &req, func(user editUser, t target) (StoredPlan, error) {
		to := t.index - 1
		if req.Direction == "down" {
			to = t.index + 1
		}
		return a.svc.MoveExercise(c.Request().Context(), user, t.plan, t.day, t.index, to)
	})
}

func (a *API) setPrescription(c *echo.Context) error {
	var req PrescriptionRequest
	return a.edit(c, true, &req, func(user editUser, t target) (StoredPlan, error) {
		return a.svc.SetPrescription(c.Request().Context(), user, t.plan, t.day, t.index, req.Sets, req.Reps, req.RestSeconds)
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
func (a *API) edit(c *echo.Context, withIndex bool, body any, run func(editUser, target) (StoredPlan, error)) error {
	t, err := parseAPITarget(c, withIndex)
	if err != nil {
		return err
	}
	if body != nil {
		if err = echox.BindJSON(c, body, httpx.ReadOptions{MaxBytes: 4 << 10}, "The request body could not be read."); err != nil {
			return err
		}
	}
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	edited, err := run(user, t)
	switch {
	case err == nil:
		return a.writePlan(c, edited.ID, http.StatusOK)
	case apperr.Is(err, ErrPlanSuperseded):
		// Answer with the newest version, so the client can show what is
		// actually there before trying again.
		current, latestErr := a.svc.CurrentVersionOf(ctx, user, t.plan)
		if latestErr != nil {
			return httpx.Fail(latestErr, "That plan was not found.")
		}
		stored, problems, detailErr := a.svc.PlanForDisplay(ctx, current.ID, user.ID)
		if detailErr != nil {
			return httpx.Fail(detailErr, "That plan was not found.")
		}
		progress, progressErr := a.svc.WeekProgress(ctx, user, time.Now())
		if progressErr != nil {
			return httpx.Fail(progressErr, "That plan could not be loaded.")
		}
		active, activeErr := a.svc.ActivePlan(ctx, user.ID)
		if activeErr != nil {
			return httpx.Fail(activeErr, "That plan could not be loaded.")
		}
		return c.JSON(http.StatusConflict, projectDetail(stored, problems, progress, active.IntakeID))
	case apperr.Is(err, apperr.ErrValidation):
		// The plan package's own words: "an exercise needs at least one set".
		message := strings.TrimSuffix(err.Error(), ": "+apperr.ErrValidation.Error())
		return httpx.Fail(apperr.FieldErrors{}.Add("plan", message), message)
	default:
		return httpx.Fail(err, "That change could not be saved.")
	}
}

// MARK: Suggestions

// Both suggestion routes take ?q=: a typed query searches the catalog among
// the equipment the person has, as the web picker does; an empty one answers
// with the ranked suggestions.

func (a *API) suggestForDay(c *echo.Context) error {
	t, err := parseAPITarget(c, false)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	if q := strings.TrimSpace(c.QueryParam("q")); q != "" {
		found, searchErr := a.svc.SearchCatalog(ctx, user, q)
		return writeExercises(c, found, searchErr)
	}
	found, err := a.svc.SuggestForDay(ctx, user, t.plan, t.day)
	return writeExercises(c, found, err)
}

func (a *API) suggestReplacements(c *echo.Context) error {
	t, err := parseAPITarget(c, true)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	if q := strings.TrimSpace(c.QueryParam("q")); q != "" {
		found, searchErr := a.svc.SearchCatalog(ctx, user, q)
		return writeExercises(c, found, searchErr)
	}
	found, err := a.svc.SuggestReplacements(ctx, user, t.plan, t.day, t.index)
	return writeExercises(c, found, err)
}

func writeExercises(c *echo.Context, found []exercise.Exercise, err error) error {
	if err != nil {
		return httpx.Fail(err, "Suggestions could not be loaded.")
	}
	return c.JSON(http.StatusOK, exercises.ProjectList(found))
}

// MARK: Projection

func projectSummary(p StoredPlan) PlanSummary {
	days := make([]DaySummary, 0, len(p.Plan.Days))
	for _, d := range p.Plan.Days {
		days = append(days, DaySummary{Weekday: d.Weekday, StartTime: d.StartTime, Focus: d.Focus, ExerciseCount: len(d.Exercises)})
	}
	return PlanSummary{ID: p.ID, Name: p.Plan.Name, WeeksTotal: p.Plan.WeeksTotal, Days: days, Source: sourceOf(p), CreatedAt: p.CreatedAt}
}

func projectDetail(p StoredPlan, problems []string, progress WeekProgress, active uuid.UUID) PlanDetail {
	days := make([]Day, 0, len(p.Plan.Days))
	for dayIndex, d := range p.Plan.Days {
		exercises := make([]DayExercise, 0, len(d.Exercises))
		lighter := progress.Lighter(p.IntakeID, dayIndex)
		volume := progress.Volume
		if lighter {
			volume = VolumeDeload
		}
		for i, e := range d.Exercises {
			row := DayExercise{
				Name: e.Name, Sets: e.Sets, ThisWeekSets: volume.SetsFor(i, e.Sets), Reps: e.Reps, Load: e.Load, RestSeconds: e.RestSeconds, Equipment: e.Equipment,
				FormCues: e.FormCues, Substitute: e.Substitute, CatalogSlug: e.CatalogSlug, HasArt: e.HasIllustration(),
				Primary: nonNil(e.Primary), Secondary: nonNil(e.Secondary), Stabilizers: nonNil(e.Stabilizers),
			}
			if row.HasArt {
				row.IllustrationSlug = e.IllustrationSlug
			}
			exercises = append(exercises, row)
		}
		days = append(days, Day{
			Weekday: d.Weekday, StartTime: d.StartTime, Focus: d.Focus, Exercises: exercises,
			CompletedThisWeek: progress.Trained(p.IntakeID, dayIndex), IsNext: progress.IsNext(p.IntakeID, dayIndex),
			Lighter: lighter,
		})
	}
	if problems == nil {
		problems = []string{}
	}
	return PlanDetail{
		ID: p.ID, Name: p.Plan.Name, Rationale: p.Plan.Rationale, WeeksTotal: p.Plan.WeeksTotal,
		Days: days, Problems: problems, Source: sourceOf(p), CreatedAt: p.CreatedAt,
		WeekVolume: string(weekVolume(progress.Volume)), Active: p.IntakeID == active,
	}
}

// weekVolume names a week without a review as hold, which is what it is.
func weekVolume(v Volume) Volume {
	if v.Valid() {
		return v
	}
	return VolumeHold
}

func sourceOf(p StoredPlan) string {
	if p.Source == "" {
		return SourceAI
	}
	return p.Source
}

func parseAPITarget(c *echo.Context, withIndex bool) (target, error) {
	id, err := echox.UUIDParam(c, "planID")
	if err != nil {
		return target{}, err
	}
	day, err := strconv.Atoi(c.Param("day"))
	if err != nil || day < 0 {
		return target{}, httpx.Fail(apperr.ErrNotFound, "No such day.")
	}
	t := target{plan: id, day: day, index: -1}
	if withIndex {
		index, err := strconv.Atoi(c.Param("index"))
		if err != nil || index < 0 {
			return target{}, httpx.Fail(apperr.ErrNotFound, "No such exercise.")
		}
		t.index = index
	}
	return t, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
