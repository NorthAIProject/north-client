package workouts

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/exercises/exercise"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
	workoutpages "github.com/NorthAIProject/north-client/web/workouts"

	"github.com/a-h/templ"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Routes mounts the training endpoints. Must be behind RequireAuth.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/training", h.index)
	r.Get("/training/new", h.showIntake)
	// A static segment beside /training/{id}, exactly as /training/new already
	// is: chi resolves static before param, and a plan id is a UUID, so the two
	// can never collide.
	r.Get("/training/plans", h.listPlans)
	r.Post("/training/new", h.submitIntake)
	r.Get("/training/{id}", h.showPlan)

	// Editing. Every mutation inserts a new plan row rather than updating one,
	// so these all respond with the re-rendered day card for the *new* plan and
	// push its URL. See Service.applyEdit.
	r.Get("/training/{id}/days/{day}", h.dayFragment)
	r.Get("/training/{id}/days/{day}/add", h.addPanel)
	r.Get("/training/{id}/days/{day}/exercises/{index}/swap", h.swapPanel)
	r.Post("/training/{id}/days/{day}/exercises", h.addExercise)
	r.Post("/training/{id}/days/{day}/exercises/{index}/swap", h.swapExercise)
	r.Post("/training/{id}/days/{day}/exercises/{index}/remove", h.removeExercise)
	r.Post("/training/{id}/days/{day}/exercises/{index}/move", h.moveExercise)
	r.Post("/training/{id}/days/{day}/exercises/{index}/sets", h.setPrescription)
}

// editTarget is the position an edit names, parsed from the URL.
type editTarget struct {
	planID uuid.UUID
	day    int
	index  int
}

// parseTarget reads the plan, day and exercise out of the path.
//
// Every part of it is user-supplied, so a malformed one is a bad request rather
// than something to trust into a slice index. plan.Swap bounds-checks again;
// this only rejects what cannot be a position at all.
func parseTarget(r *http.Request, withIndex bool) (editTarget, error) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return editTarget{}, apperr.ErrNotFound
	}

	day, err := strconv.Atoi(chi.URLParam(r, "day"))
	if err != nil || day < 0 {
		return editTarget{}, apperr.ErrValidation
	}

	target := editTarget{planID: id, day: day, index: -1}
	if !withIndex {
		return target, nil
	}

	index, err := strconv.Atoi(chi.URLParam(r, "index"))
	if err != nil || index < 0 {
		return editTarget{}, apperr.ErrValidation
	}
	target.index = index
	return target, nil
}

// dayFragment re-renders one day card. It backs the picker's Cancel, which has
// to put the original row back without reloading the page.
func (h *Handler) dayFragment(w http.ResponseWriter, r *http.Request) {
	target, err := parseTarget(r, false)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	view, day, err := h.dayOf(r, target)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	render(w, r, http.StatusOK, workoutpages.DayCard(view, target.day, day))
}

// swapPanel expands one exercise into the replacement picker.
func (h *Handler) swapPanel(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	ctx := r.Context()

	target, err := parseTarget(r, true)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	view, day, err := h.dayOf(r, target)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if target.index >= len(day.Exercises) {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	picker := workoutpages.SwapPicker(view, target.day, target.index, day.Exercises[target.index].Name)
	if picker, err = h.fillPicker(ctx, user, r, picker, func() ([]exercise.Exercise, error) {
		return h.svc.SuggestReplacements(ctx, user, target.planID, target.day, target.index)
	}); err != nil {
		h.fail(w, r, err)
		return
	}

	render(w, r, http.StatusOK, workoutpages.DayCardSwapping(view, target.day, day, target.index, picker))
}

// addPanel expands a day into the picker that chooses something to append.
func (h *Handler) addPanel(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	ctx := r.Context()

	target, err := parseTarget(r, false)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	view, day, err := h.dayOf(r, target)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	picker := workoutpages.AddPicker(view, target.day, day.Weekday)
	if picker, err = h.fillPicker(ctx, user, r, picker, func() ([]exercise.Exercise, error) {
		return h.svc.SuggestForDay(ctx, user, target.planID, target.day)
	}); err != nil {
		h.fail(w, r, err)
		return
	}

	render(w, r, http.StatusOK, workoutpages.DayCardAdding(view, target.day, day, picker))
}

// fillPicker puts the options in a configured panel.
//
// A typed query searches the catalog; an empty one shows the ranked
// suggestions, which is the case that answers most of these without anyone
// having to think of an exercise name.
func (h *Handler) fillPicker(ctx context.Context, user users.User, r *http.Request, picker workoutpages.Picker, suggest func() ([]exercise.Exercise, error)) (workoutpages.Picker, error) {
	picker.Query = strings.TrimSpace(r.URL.Query().Get("q"))

	var err error
	if picker.Query == "" {
		picker.Suggestions, err = suggest()
	} else {
		picker.Matches, err = h.svc.SearchCatalog(ctx, user, picker.Query)
	}
	return picker, err
}

// addExercise appends a catalog exercise to a day.
func (h *Handler) addExercise(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	ctx := r.Context()

	target, err := parseTarget(r, false)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if err = r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	edited, err := h.svc.AddExercise(ctx, user, target.planID, target.day, r.PostFormValue("catalog_slug"))
	if err != nil {
		h.afterEditError(w, r, user, target.planID, err)
		return
	}

	h.afterEdit(w, r, edited.ID)
}

// removeExercise drops one exercise from a day.
func (h *Handler) removeExercise(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	target, err := parseTarget(r, true)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	edited, err := h.svc.RemoveExercise(r.Context(), user, target.planID, target.day, target.index)
	if err != nil {
		h.afterEditError(w, r, user, target.planID, err)
		return
	}

	h.afterEdit(w, r, edited.ID)
}

// swapExercise applies the swap and returns the day as it now stands.
func (h *Handler) swapExercise(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	ctx := r.Context()

	target, err := parseTarget(r, true)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if err = r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	edited, err := h.svc.SwapExercise(ctx, user, target.planID, target.day, target.index, r.PostFormValue("catalog_slug"))
	if err != nil {
		h.afterEditError(w, r, user, target.planID, err)
		return
	}

	h.afterEdit(w, r, edited.ID)
}

// moveExercise reorders one exercise within its day.
//
// Up and down rather than a target index: the buttons are what the interface
// offers, and translating here means a hand-built request cannot ask for a
// position no button could have produced.
func (h *Handler) moveExercise(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	target, err := parseTarget(r, true)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if err = r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	to := target.index - 1
	if r.PostFormValue("direction") == "down" {
		to = target.index + 1
	}

	edited, err := h.svc.MoveExercise(r.Context(), user, target.planID, target.day, target.index, to)
	if err != nil {
		h.afterEditError(w, r, user, target.planID, err)
		return
	}

	h.afterEdit(w, r, edited.ID)
}

// setPrescription changes how much of an exercise to do.
func (h *Handler) setPrescription(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	target, err := parseTarget(r, true)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if err = r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	// atoiOr rather than a parse error: an empty or unreadable number becomes a
	// value the plan package then rejects with a message about training, which
	// is more use than "invalid syntax".
	edited, err := h.svc.SetPrescription(r.Context(), user, target.planID, target.day, target.index,
		atoiOr(r.PostFormValue("sets"), 0),
		r.PostFormValue("reps"),
		atoiOr(r.PostFormValue("rest_seconds"), -1),
	)
	if err != nil {
		h.afterEditError(w, r, user, target.planID, err)
		return
	}

	h.afterEdit(w, r, edited.ID)
}

// afterEdit responds with the plan the edit produced.
//
// The edit created a new plan, so the URL in the address bar now names a
// superseded one. Pushing the new id means a reload shows what is on screen.
func (h *Handler) afterEdit(w http.ResponseWriter, r *http.Request, planID uuid.UUID) {
	w.Header().Set("HX-Push-Url", "/app/training/"+planID.String())
	h.renderPlanBody(w, r, planID, http.StatusOK)
}

// afterEditError handles a refused edit.
//
// A superseded plan is not an error the person caused — another tab, or a card
// that had not been re-rendered yet, got there first. Recovering means
// rendering the plan that is actually current, not the one the request named:
// re-rendering the stale one would hand back the same superseded id and refuse
// the next click exactly as it refused this one, forever.
func (h *Handler) afterEditError(w http.ResponseWriter, r *http.Request, user users.User, planID uuid.UUID, err error) {
	if !apperr.Is(err, ErrPlanSuperseded) {
		h.fail(w, r, err)
		return
	}

	// The newest version of the plan that was named, not the account's newest
	// row: someone editing an older plan from the plans list must be sent back
	// to that plan, not to whichever one they generated most recently.
	current, latestErr := h.svc.CurrentVersionOf(r.Context(), user, planID)
	if latestErr != nil {
		h.fail(w, r, latestErr)
		return
	}

	w.Header().Set("HX-Push-Url", "/app/training/"+current.ID.String())
	h.renderPlanBody(w, r, current.ID, http.StatusConflict)
}

// renderPlanBody responds with everything an edit can change.
//
// The whole body rather than the edited day: an edit inserts a new plan row, so
// every day card's URLs — which embed the plan id — go stale at once. It also
// carries the validation notice and the licence credit, neither of which a
// day-card swap could reach.
func (h *Handler) renderPlanBody(w http.ResponseWriter, r *http.Request, planID uuid.UUID, status int) {
	user := auth.MustUser(r.Context())

	stored, problems, err := h.svc.PlanForDisplay(r.Context(), planID, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	render(w, r, status, workoutpages.PlanBody(workoutpages.PlanView{
		ID:        stored.ID,
		Plan:      stored.Plan,
		CreatedAt: stored.CreatedAt,
		Problems:  problems,
	}))
}

// dayOf loads a plan for display and picks out one day.
func (h *Handler) dayOf(r *http.Request, target editTarget) (workoutpages.PlanView, plan.PlanDay, error) {
	user := auth.MustUser(r.Context())

	stored, problems, err := h.svc.PlanForDisplay(r.Context(), target.planID, user.ID)
	if err != nil {
		return workoutpages.PlanView{}, plan.PlanDay{}, err
	}
	if target.day >= len(stored.Plan.Days) {
		return workoutpages.PlanView{}, plan.PlanDay{}, apperr.ErrValidation
	}

	view := workoutpages.PlanView{
		ID:        stored.ID,
		Plan:      stored.Plan,
		CreatedAt: stored.CreatedAt,
		Problems:  problems,
	}
	return view, stored.Plan.Days[target.day], nil
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	plans, err := h.svc.ListPlans(r.Context(), user.ID, 20)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	if len(plans) == 0 {
		http.Redirect(w, r, "/app/training/new", http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, "/app/training/"+plans[0].ID.String(), http.StatusSeeOther)
}

// listPlans shows every plan someone has.
//
// It exists because /app/training resolves to the newest plan and nothing
// linked anywhere else, so generating a second plan made the first unreachable
// — still stored, still rendered by /app/training/{id}, but only for whoever
// kept the URL.
//
// ListCurrentPlans rather than ListPlans: since editing, a plan is several rows
// and this page is a list of plans, not of versions.
func (h *Handler) listPlans(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	plans, err := h.svc.ListCurrentPlans(r.Context(), user.ID, 0)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	// Nothing to list means nothing has been built yet, which is the intake's
	// job — the same redirect index already makes.
	if len(plans) == 0 {
		http.Redirect(w, r, "/app/training/new", http.StatusSeeOther)
		return
	}

	views := make([]workoutpages.PlanSummary, 0, len(plans))
	for _, stored := range plans {
		views = append(views, workoutpages.PlanSummary{
			ID:        stored.ID,
			Name:      stored.Plan.Name,
			Weeks:     stored.Plan.WeeksTotal,
			Days:      len(stored.Plan.Days),
			CreatedAt: stored.CreatedAt,
		})
	}

	render(w, r, http.StatusOK, workoutpages.PlansPage(user, views))
}

func (h *Handler) showIntake(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	form := workoutpages.IntakeForm{DaysPerWeek: 3, SessionMinutes: 45}

	// Pre-fill from the last intake, so someone regenerating a plan does not
	// answer the same questions again.
	if previous, err := h.svc.LatestIntake(r.Context(), user.ID); err == nil {
		form = workoutpages.IntakeForm{
			Goal:           previous.Intake.Goal,
			Experience:     previous.Intake.Experience,
			DaysPerWeek:    previous.Intake.DaysPerWeek,
			SessionMinutes: previous.Intake.SessionMinutes,
			Equipment:      previous.Intake.Equipment,
			Limitations:    previous.Intake.Limitations,
		}
	}

	render(w, r, http.StatusOK, workoutpages.IntakePage(user, form))
}

func (h *Handler) submitIntake(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	form := workoutpages.IntakeForm{
		Goal:           strings.TrimSpace(r.PostFormValue("goal")),
		Experience:     strings.TrimSpace(r.PostFormValue("experience")),
		DaysPerWeek:    atoiOr(r.PostFormValue("days_per_week"), 0),
		SessionMinutes: atoiOr(r.PostFormValue("session_minutes"), 0),
		Equipment:      r.PostForm["equipment"],
		Limitations:    strings.TrimSpace(r.PostFormValue("limitations")),
	}

	plan, err := h.svc.CreatePlan(r.Context(), user, Intake{
		Goal:           form.Goal,
		Experience:     form.Experience,
		DaysPerWeek:    form.DaysPerWeek,
		SessionMinutes: form.SessionMinutes,
		Equipment:      form.Equipment,
		Limitations:    form.Limitations,
	})
	if err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			render(w, r, http.StatusUnprocessableEntity, workoutpages.IntakePage(user, form))
			return
		}

		middleware.FromContext(r.Context()).Error("plan generation failed", slog.Any("error", err))

		// The intake was kept, so this is recoverable by trying again rather
		// than by re-entering everything.
		form.Error = "Khepri could not build a plan that fits those constraints. Try widening them slightly, or try again."
		if apperr.Is(err, apperr.ErrUnavailable) {
			form.Error = "The coach is busy right now. Your answers were saved — try again in a moment."
		}
		render(w, r, http.StatusServiceUnavailable, workoutpages.IntakePage(user, form))
		return
	}

	http.Redirect(w, r, "/app/training/"+plan.ID.String(), http.StatusSeeOther)
}

func (h *Handler) showPlan(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	stored, problems, err := h.svc.PlanForDisplay(r.Context(), id, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	render(w, r, http.StatusOK, workoutpages.PlanPage(user, workoutpages.PlanView{
		ID:        stored.ID,
		Plan:      stored.Plan,
		CreatedAt: stored.CreatedAt,
		Problems:  problems,
	}))
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case apperr.Is(err, apperr.ErrNotFound):
		http.Error(w, "Not found.", http.StatusNotFound)
	case apperr.Is(err, ErrPlanSuperseded):
		http.Error(w, "This plan has been edited since it was loaded.", http.StatusConflict)
	case apperr.Is(err, apperr.ErrValidation):
		http.Error(w, "That request could not be read.", http.StatusUnprocessableEntity)
	default:
		middleware.FromContext(r.Context()).Error("training request failed", slog.Any("error", err))
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
	}
}

func render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)

	if err := c.Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render failed", slog.Any("error", err))
	}
}

func atoiOr(s string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return fallback
	}
	return n
}
