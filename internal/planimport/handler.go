package planimport

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/quota"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	pages "github.com/NorthAIProject/north-client/web/planimport"
)

// Handler serves the web import pages: upload, review, save.
//
// Plain form posts throughout. Every button on the review page posts the
// whole draft and gets the whole page back, which is the same round trip the
// meal plan pages already make, and keeps a half-reviewed import alive in the
// browser rather than in a table nobody would ever clean up.
type Handler struct {
	svc    *Service
	quotas *quota.Service
}

func NewHandler(svc *Service, quotas *quota.Service) *Handler {
	return &Handler{svc: svc, quotas: quotas}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/training/import", h.workoutUpload)
	r.With(h.quotas.Guard(quota.PlanImport)).Post("/training/import", h.workoutParse)
	r.Post("/training/import/review", h.workoutReview)

	r.Get("/nutrition/import", h.mealUpload)
	r.With(h.quotas.Guard(quota.PlanImport)).Post("/nutrition/import", h.mealParse)
	r.Post("/nutrition/import/review", h.mealReview)

	r.Get("/nutrition/plans/generate", h.mealGenerateForm)
	r.With(h.quotas.Guard(quota.MealPlanGenerate)).Post("/nutrition/plans/generate", h.mealGenerate)
}

func (h *Handler) workoutUpload(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, pages.UploadPage(auth.MustUser(r.Context()), pages.Workout, ""))
}

func (h *Handler) mealUpload(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, pages.UploadPage(auth.MustUser(r.Context()), pages.Meal, ""))
}

func (h *Handler) workoutParse(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	name, data, err := formUpload(r)
	if err == nil {
		var d WorkoutDraft
		if d, err = h.svc.ParseWorkout(r.Context(), user, name, data, ""); err == nil {
			h.render(w, r, http.StatusOK, pages.WorkoutReviewPage(user, pages.WorkoutReview{Draft: d, DraftJSON: encode(d)}))
			return
		}
	}
	h.logFailure(r, "plan import: workout file refused", err)
	h.render(w, r, httpx.Status(err), pages.UploadPage(user, pages.Workout, message(r.Context(), err)))
}

func (h *Handler) mealParse(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	name, data, err := formUpload(r)
	if err == nil {
		var d MealDraft
		if d, err = h.svc.ParseMeal(r.Context(), user, name, data, ""); err == nil {
			h.render(w, r, http.StatusOK, pages.MealReviewPage(user, h.mealView(r.Context(), d, nil)))
			return
		}
	}
	h.logFailure(r, "plan import: meal file refused", err)
	h.render(w, r, httpx.Status(err), pages.UploadPage(user, pages.Meal, message(r.Context(), err)))
}

func (h *Handler) workoutReview(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	d, action, err := workoutFromForm(r)
	if err != nil {
		h.render(w, r, http.StatusUnprocessableEntity, pages.UploadPage(user, pages.Workout, i18n.T(r.Context(), "planimport.err.expired")))
		return
	}
	applyWorkoutAction(&d, action)

	if action.verb == "save" {
		stored, err := h.svc.CommitWorkout(r.Context(), user, d)
		if err == nil {
			http.Redirect(w, r, "/app/training/"+stored.ID.String(), http.StatusSeeOther)
			return
		}
		h.render(w, r, httpx.Status(err), pages.WorkoutReviewPage(user, pages.WorkoutReview{Draft: d, DraftJSON: encode(d), Error: message(r.Context(), err)}))
		return
	}

	h.render(w, r, http.StatusOK, pages.WorkoutReviewPage(user, pages.WorkoutReview{Draft: d, DraftJSON: encode(d)}))
}

func (h *Handler) mealReview(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	d, action, err := mealFromForm(r)
	if err != nil {
		h.render(w, r, http.StatusUnprocessableEntity, pages.UploadPage(user, pages.Meal, i18n.T(r.Context(), "planimport.err.expired")))
		return
	}
	applyMealAction(&d, action)

	if action.verb == "save" || action.verb == "save-confirm" {
		saved, previewed, err := h.svc.CommitMeal(r.Context(), user, d, action.verb == "save-confirm")
		if err == nil {
			http.Redirect(w, r, "/app/nutrition/plans/"+saved.ID.String(), http.StatusSeeOther)
			return
		}
		h.render(w, r, httpx.Status(err), pages.MealReviewPage(user, h.mealView(r.Context(), previewed, err)))
		return
	}

	d = h.svc.PreviewMeal(r.Context(), user.ID, d)
	h.render(w, r, http.StatusOK, pages.MealReviewPage(user, h.mealView(r.Context(), d, nil)))
}

// mealView assembles the page data, turning a refused save into what the
// page shows: the overage to confirm, or the reason to fix.
func (h *Handler) mealView(ctx context.Context, d MealDraft, err error) pages.MealReview {
	view := pages.MealReview{Draft: d, DraftJSON: encode(d)}
	var over *meals.OverageError
	switch {
	case err == nil:
	case apperr.As(err, &over) && over.Verdict.CanConfirm:
		view.NeedsConfirm = true
	case apperr.As(err, &over):
		view.Error = i18n.Tf(ctx, "planimport.err.overeasy", over.MessageIn(ctx))
	default:
		view.Error = message(ctx, err)
	}
	return view
}

func formUpload(r *http.Request) (string, []byte, error) {
	// The CSRF middleware has already parsed the multipart body to find its
	// token; this reads the file it left in memory or on disk.
	file, header, err := r.FormFile("file")
	if err != nil {
		return "", nil, apperr.FieldErrors{}.Add("file", i18n.T(r.Context(), "planimport.err.nofile"))
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, MaxBytes+1))
	if err != nil {
		return "", nil, apperr.FieldErrors{}.Add("file", i18n.T(r.Context(), "planimport.err.upload"))
	}
	return header.Filename, data, nil
}

// message is the sentence to show for a refusal. Only validation and
// conflicts carry one meant for the person; anything else is a fault on our
// side and says so in general terms. A file's own refusal is in English;
// everything else is in ctx's language.
func message(ctx context.Context, err error) string {
	var fe *FileError
	if apperr.As(err, &fe) {
		return fe.Message
	}
	var fields apperr.FieldErrors
	if apperr.As(meals.Localize(ctx, err), &fields) && len(fields) > 0 {
		return fields[0].Message
	}
	if apperr.Is(err, apperr.ErrValidation) {
		return i18n.T(ctx, "planimport.err.invalid")
	}
	return i18n.T(ctx, "planimport.err.failed")
}

func encode(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func (h *Handler) logFailure(r *http.Request, msg string, err error) {
	level := slog.LevelInfo
	if httpx.Status(err) >= http.StatusInternalServerError {
		level = slog.LevelError
	}
	middleware.FromContext(r.Context()).Log(r.Context(), level, msg, slog.Any("error", err))
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render plan import", slog.Any("error", err))
	}
}
