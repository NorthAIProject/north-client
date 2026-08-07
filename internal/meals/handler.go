package meals

import (
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// Handler serves the nutrition pages: ingredients, meal plans, and the food
// log/progress view. One struct for all three, matching the single shared
// Repository underneath — split across files by concern, same as
// repository_*.go.
type Handler struct {
	ingredients *IngredientService
	diets       *DietPreferenceService
	plans       *MealPlanService
	foodLog     *FoodLogService
	progress    *TrackMealProgressService
	recommend   *GoalRecommendationService
}

type HandlerOptions struct {
	Ingredients *IngredientService
	Diets       *DietPreferenceService
	Plans       *MealPlanService
	FoodLog     *FoodLogService
	Progress    *TrackMealProgressService
	Recommend   *GoalRecommendationService
}

func NewHandler(opts HandlerOptions) *Handler {
	return &Handler{
		ingredients: opts.Ingredients, diets: opts.Diets, plans: opts.Plans,
		foodLog: opts.FoodLog, progress: opts.Progress, recommend: opts.Recommend,
	}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/nutrition/ingredients", h.ingredientsIndex)
	r.Post("/nutrition/ingredients", h.createIngredient)
	r.Post("/nutrition/ingredients/{id}/delete", h.deleteIngredient)

	r.Get("/nutrition/plans", h.plansIndex)
	r.Post("/nutrition/plans", h.createPlan)
	r.Get("/nutrition/plans/{id}", h.planDetail)
	r.Post("/nutrition/plans/{id}/delete", h.deletePlan)
	r.Post("/nutrition/plans/{id}/meals", h.addMeal)
	r.Post("/nutrition/meals/{mealID}/delete", h.removeMeal)
	r.Post("/nutrition/meals/{mealID}/ingredients", h.addIngredientToMeal)
	r.Post("/nutrition/meal-ingredients/{id}/delete", h.removeIngredientFromMeal)

	r.Get("/nutrition/log", h.logIndex)
	r.Post("/nutrition/log/ingredients", h.logIngredient)
	r.Post("/nutrition/log/meals", h.logMeal)
	r.Post("/nutrition/log/{id}/delete", h.deleteLogEntry)
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		middleware.FromContext(r.Context()).Error("render failed", slog.Any("error", err))
	}
}

func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case apperr.Is(err, apperr.ErrNotFound):
		http.Error(w, "Not found.", http.StatusNotFound)
	case apperr.Is(err, apperr.ErrValidation):
		http.Error(w, "That request could not be read.", http.StatusUnprocessableEntity)
	case apperr.Is(err, apperr.ErrConflict):
		http.Error(w, "That conflicts with existing data.", http.StatusConflict)
	default:
		middleware.FromContext(r.Context()).Error("nutrition request failed", slog.Any("error", err))
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
	}
}
