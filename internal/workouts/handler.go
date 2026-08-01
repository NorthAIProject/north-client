package workouts

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
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
	r.Post("/training/new", h.submitIntake)
	r.Get("/training/{id}", h.showPlan)
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
		form.Error = "North could not build a plan that fits those constraints. Try widening them slightly, or try again."
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

	plan, err := h.svc.GetPlan(r.Context(), id, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	render(w, r, http.StatusOK, workoutpages.PlanPage(user, plan.Plan, plan.CreatedAt))
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case apperr.Is(err, apperr.ErrNotFound):
		http.Error(w, "Not found.", http.StatusNotFound)
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
