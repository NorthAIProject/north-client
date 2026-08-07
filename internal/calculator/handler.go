package calculator

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	calculatorpages "github.com/NorthAIProject/north-client/web/calculator"
)

// Handler serves the calculator page. It depends on biometrics.Service
// directly rather than through biometrics having its own handler: biometrics
// has exactly one caller today (this page), so a standalone page for it
// would be structure with nothing yet asking for it.
type Handler struct {
	svc        *Service
	biometrics *biometrics.Service
}

func NewHandler(svc *Service, bio *biometrics.Service) *Handler {
	return &Handler{svc: svc, biometrics: bio}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/calculator", h.show)
	r.Post("/calculator/biometrics", h.recordBiometrics)
	r.Post("/calculator/generate", h.generate)
}

func (h *Handler) show(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, http.StatusOK, calculatorpages.BiometricsForm{}, calculatorpages.PlanForm{})
}

func (h *Handler) recordBiometrics(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	form := calculatorpages.BiometricsForm{
		WeightKg: r.PostFormValue("weight_kg"), HeightCm: r.PostFormValue("height_cm"),
		DateOfBirth: r.PostFormValue("date_of_birth"), Sex: r.PostFormValue("sex"),
	}

	weight, _ := strconv.ParseFloat(form.WeightKg, 64)
	height, _ := strconv.ParseFloat(form.HeightCm, 64)
	dob, _ := time.Parse("2006-01-02", form.DateOfBirth)

	if _, err := h.biometrics.Record(r.Context(), user.ID, biometrics.Input{
		WeightKg: weight, HeightCm: height, DateOfBirth: dob, Sex: form.Sex,
	}); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			h.render(w, r, http.StatusUnprocessableEntity, form, calculatorpages.PlanForm{})
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/calculator", http.StatusSeeOther)
}

func (h *Handler) generate(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	form := calculatorpages.PlanForm{
		ActivityLevel: r.PostFormValue("activity_level"), Goal: r.PostFormValue("goal"), MacroSplit: r.PostFormValue("macro_split"),
	}

	if _, err := h.svc.Generate(r.Context(), user.ID, Input{ActivityLevel: form.ActivityLevel, Goal: form.Goal, MacroSplit: form.MacroSplit}); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
		} else if apperr.Is(err, apperr.ErrValidation) {
			// Not a per-field failure (e.g. "record your biometrics first") —
			// shown as a general message under the form instead of a field.
			form.Errors = map[string]string{"_": err.Error()}
		} else {
			h.fail(w, r, err)
			return
		}
		h.render(w, r, http.StatusUnprocessableEntity, calculatorpages.BiometricsForm{}, form)
		return
	}

	http.Redirect(w, r, "/app/calculator", http.StatusSeeOther)
}

// render loads the current biometrics/plan (whatever the caller doesn't
// already have an error-carrying copy of) and shows the page.
func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, bioForm calculatorpages.BiometricsForm, planForm calculatorpages.PlanForm) {
	user := auth.MustUser(r.Context())
	ctx := r.Context()

	bio, err := h.biometrics.Current(ctx, user.ID)
	hasBio := true
	if err != nil {
		if !apperr.Is(err, apperr.ErrNotFound) {
			h.fail(w, r, err)
			return
		}
		hasBio = false
	}

	plan, err := h.svc.Current(ctx, user.ID)
	hasPlan := true
	if err != nil {
		if !apperr.Is(err, apperr.ErrNotFound) {
			h.fail(w, r, err)
			return
		}
		hasPlan = false
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := calculatorpages.Page(user, bio, hasBio, plan, hasPlan, bioForm, planForm).Render(ctx, w); err != nil {
		middleware.FromContext(ctx).Error("render calculator", slog.Any("error", err))
	}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	middleware.FromContext(r.Context()).Error("calculator request failed", slog.Any("error", err))
	http.Error(w, "Something went wrong.", http.StatusInternalServerError)
}
