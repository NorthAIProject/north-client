package goals

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/analytics"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/goals/goal"
	"github.com/NorthAIProject/north-client/internal/moments"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/users"
	goalpages "github.com/NorthAIProject/north-client/web/goals"
)

type Handler struct {
	svc *Service

	// funnel reports moments shown. Nil is a no-op.
	funnel *analytics.Funnel
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// WithFunnel reports moments shown. A nil funnel is a no-op.
func (h *Handler) WithFunnel(f *analytics.Funnel) *Handler {
	h.funnel = f
	return h
}

// Routes mounts the goal endpoints. Must be behind RequireAuth.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/goals", h.index)
	r.Post("/goals", h.create)
	r.Get("/goals/{id}", h.show)
	r.Post("/goals/{id}", h.update)
	r.Post("/goals/{id}/status", h.setStatus)
	r.Post("/goals/{id}/updates", h.addUpdate)
	r.Post("/goals/{id}/milestones", h.addMilestone)
	r.Post("/goals/{id}/milestones/{mid}", h.updateMilestone)
	r.Post("/goals/{id}/milestones/{mid}/status", h.setMilestoneStatus)
	r.Post("/goals/{id}/milestones/{mid}/delete", h.deleteMilestone)
	r.Post("/goals/{id}/delete", h.destroy)
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	list, err := h.svc.List(r.Context(), user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	overdue, err := h.svc.CountOverdueMilestones(r.Context(), user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	inst, err := buildInstruments(list, overdue)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	render(w, r, http.StatusOK, goalpages.IndexPage(user, list, inst, goalpages.GoalForm{}))
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	form := formFrom(r)

	goal, err := h.svc.Create(r.Context(), user.ID, inputFrom(form))
	if err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			form.Open = true

			list, listErr := h.svc.List(r.Context(), user.ID)
			if listErr != nil {
				h.fail(w, r, listErr)
				return
			}
			overdue, overdueErr := h.svc.CountOverdueMilestones(r.Context(), user.ID)
			if overdueErr != nil {
				h.fail(w, r, overdueErr)
				return
			}
			inst, instErr := buildInstruments(list, overdue)
			if instErr != nil {
				h.fail(w, r, instErr)
				return
			}
			render(w, r, http.StatusUnprocessableEntity, goalpages.IndexPage(user, list, inst, form))
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/goals/"+goal.ID.String(), http.StatusSeeOther)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	goal, updates, err := h.loadDetail(r.Context(), id, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	render(w, r, http.StatusOK, goalpages.DetailPage(user, goal, updates, goalpages.FormFor(goal), h.momentFrom(r, user, goal)))
}

// momentFrom reads the ?done= marker a status change redirects with and turns
// it into the card the page shows once. The URL is the flash: there is no
// store to clear, and a reload shows the card again, which is the harmless
// outcome. The funnel event fires only when a card is actually rendered.
//
// A milestone id that is not on this goal is ignored rather than failed; the
// person marked something done and landed on the right page, which is what
// matters.
func (h *Handler) momentFrom(r *http.Request, user users.User, g goal.Goal) *moments.Moment {
	done := r.URL.Query().Get("done")
	var m moments.Moment
	switch {
	case done == "goal":
		m = moments.ForGoalAchieved(g.Title)
	case strings.HasPrefix(done, "milestone:"):
		mid, err := uuid.Parse(strings.TrimPrefix(done, "milestone:"))
		if err != nil {
			return nil
		}
		var found *goal.Milestone
		for i := range g.Milestones {
			if g.Milestones[i].ID == mid {
				found = &g.Milestones[i]
				break
			}
		}
		if found == nil {
			return nil
		}
		m = moments.ForMilestoneCompleted(g.Title, found.Title)
	default:
		return nil
	}
	h.funnel.MomentShown(r.Context(), user.ID, m.Kind)
	return &m
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	form := formFrom(r)

	if _, err := h.svc.Update(r.Context(), id, user.ID, inputFrom(form)); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			goal, updates, loadErr := h.loadDetail(r.Context(), id, user.ID)
			if loadErr != nil {
				h.fail(w, r, loadErr)
				return
			}

			form.Errors = fieldErrs.Messages()
			form.Open = true
			render(w, r, http.StatusUnprocessableEntity, goalpages.DetailPage(user, goal, updates, form, nil))
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/goals/"+id.String(), http.StatusSeeOther)
}

func (h *Handler) setStatus(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	status := r.PostFormValue("status")
	if _, err := h.svc.SetStatus(r.Context(), id, user.ID, status); err != nil {
		h.fail(w, r, err)
		return
	}

	dest := "/app/goals/" + id.String()
	if status == goal.StatusAchieved {
		dest += "?done=goal"
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

func (h *Handler) addUpdate(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	var progress *int
	if raw := strings.TrimSpace(r.PostFormValue("progress")); raw != "" {
		if n, convErr := strconv.Atoi(raw); convErr == nil {
			progress = &n
		}
	}

	if _, err := h.svc.AddUpdate(r.Context(), id, user.ID, r.PostFormValue("note"), progress); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			goal, updates, loadErr := h.loadDetail(r.Context(), id, user.ID)
			if loadErr != nil {
				h.fail(w, r, loadErr)
				return
			}

			form := goalpages.FormFor(goal)
			form.UpdateError = fieldErrs.Messages()["note"]
			render(w, r, http.StatusUnprocessableEntity, goalpages.DetailPage(user, goal, updates, form, nil))
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/goals/"+id.String(), http.StatusSeeOther)
}

func (h *Handler) addMilestone(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	in, submitted := milestoneFrom(r)
	if _, err := h.svc.AddMilestone(r.Context(), id, user.ID, in); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			goal, updates, loadErr := h.loadDetail(r.Context(), id, user.ID)
			if loadErr != nil {
				h.fail(w, r, loadErr)
				return
			}
			form := goalpages.FormFor(goal)
			form.MilestoneTitle = submitted.MilestoneTitle
			form.MilestoneDate = submitted.MilestoneDate
			form.MilestoneErrors = fieldErrs.Messages()
			form.MilestoneOpen = true
			render(w, r, http.StatusUnprocessableEntity, goalpages.DetailPage(user, goal, updates, form, nil))
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/goals/"+id.String(), http.StatusSeeOther)
}

func (h *Handler) updateMilestone(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, mid, ok := parseGoalAndMilestone(w, r, h)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	in, submitted := milestoneFrom(r)
	if _, err := h.svc.UpdateMilestone(r.Context(), mid, user.ID, in); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			goal, updates, loadErr := h.loadDetail(r.Context(), id, user.ID)
			if loadErr != nil {
				h.fail(w, r, loadErr)
				return
			}
			form := goalpages.FormFor(goal)
			form.MilestoneTitle = submitted.MilestoneTitle
			form.MilestoneDate = submitted.MilestoneDate
			form.MilestoneErrors = fieldErrs.Messages()
			form.EditingMilestone = mid.String()
			render(w, r, http.StatusUnprocessableEntity, goalpages.DetailPage(user, goal, updates, form, nil))
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/goals/"+id.String(), http.StatusSeeOther)
}

func (h *Handler) setMilestoneStatus(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, mid, ok := parseGoalAndMilestone(w, r, h)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	status := r.PostFormValue("status")
	if _, err := h.svc.SetMilestoneStatus(r.Context(), mid, user.ID, status); err != nil {
		h.fail(w, r, err)
		return
	}

	dest := "/app/goals/" + id.String()
	if status == goal.MilestoneCompleted {
		dest += "?done=milestone:" + mid.String()
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

func (h *Handler) deleteMilestone(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, mid, ok := parseGoalAndMilestone(w, r, h)
	if !ok {
		return
	}

	if err := h.svc.DeleteMilestone(r.Context(), mid, user.ID); err != nil {
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/goals/"+id.String(), http.StatusSeeOther)
}

func (h *Handler) destroy(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	if err := h.svc.Delete(r.Context(), id, user.ID); err != nil {
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/goals", http.StatusSeeOther)
}

func formFrom(r *http.Request) goalpages.GoalForm {
	return goalpages.GoalForm{
		Title:      strings.TrimSpace(r.PostFormValue("title")),
		Motivation: strings.TrimSpace(r.PostFormValue("motivation")),
		Success:    strings.TrimSpace(r.PostFormValue("success")),
		Category:   strings.TrimSpace(r.PostFormValue("category")),
		TargetDate: strings.TrimSpace(r.PostFormValue("target_date")),
	}
}

func inputFrom(f goalpages.GoalForm) Input {
	in := Input{
		Title:      f.Title,
		Motivation: f.Motivation,
		Success:    f.Success,
		Category:   f.Category,
	}

	// An unparseable date is treated as no date rather than as an error: the
	// input is type="date", so anything else means the browser sent nothing.
	if f.TargetDate != "" {
		if parsed, err := time.Parse("2006-01-02", f.TargetDate); err == nil {
			in.TargetDate = parsed
		}
	}

	return in
}

func milestoneFrom(r *http.Request) (MilestoneInput, goalpages.GoalForm) {
	form := formFrom(r)
	form.MilestoneTitle = strings.TrimSpace(r.PostFormValue("milestone_title"))
	form.MilestoneDate = strings.TrimSpace(r.PostFormValue("milestone_date"))

	in := MilestoneInput{Title: form.MilestoneTitle}
	if form.MilestoneDate != "" {
		if parsed, err := time.Parse("2006-01-02", form.MilestoneDate); err == nil {
			in.TargetDate = parsed
		}
	}
	return in, form
}

func parseGoalAndMilestone(w http.ResponseWriter, r *http.Request, h *Handler) (uuid.UUID, uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return uuid.UUID{}, uuid.UUID{}, false
	}
	mid, err := uuid.Parse(chi.URLParam(r, "mid"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return uuid.UUID{}, uuid.UUID{}, false
	}
	return id, mid, true
}

func (h *Handler) loadDetail(ctx context.Context, id, userID uuid.UUID) (Goal, []Update, error) {
	goal, err := h.svc.Get(ctx, id, userID)
	if err != nil {
		return Goal{}, nil, err
	}

	ms, err := h.svc.Milestones(ctx, id, userID)
	if err != nil {
		return Goal{}, nil, err
	}

	updates, err := h.svc.Updates(ctx, id, userID, 50)
	if err != nil {
		return Goal{}, nil, err
	}

	return goal.WithMilestones(ms), updates, nil
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case apperr.Is(err, apperr.ErrNotFound):
		http.Error(w, "Not found.", http.StatusNotFound)
	case apperr.Is(err, apperr.ErrValidation):
		http.Error(w, "That request could not be read.", http.StatusUnprocessableEntity)
	default:
		middleware.FromContext(r.Context()).Error("goal request failed", slog.Any("error", err))
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
