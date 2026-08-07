package meals

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	nutritionpages "github.com/NorthAIProject/north-client/web/nutrition"
)

func (h *Handler) logIndex(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	ctx := r.Context()
	today := time.Now().In(user.Location())

	entries, err := h.foodLog.Day(ctx, user.ID, today)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	progressSummary, hasProgress, err := h.progressSummary(ctx, user.ID, today)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	var recommendationMsg string
	hasRecommendation := false
	if hasProgress {
		rec, err := h.recommend.Recommend(ctx, user.ID)
		if err != nil {
			h.fail(w, r, err)
			return
		}
		recommendationMsg = rec.Message
		hasRecommendation = true
	}

	allIngredients, err := h.ingredients.Search(ctx, user.ID, "", 200)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	mealOptions, err := h.mealOptions(ctx, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	h.render(w, r, http.StatusOK, nutritionpages.LogPage(user, entries, progressSummary, hasProgress, recommendationMsg, hasRecommendation, allIngredients, mealOptions))
}

// progressSummary returns the day's progress line, or ("", false) if the
// user has no macro goal generated yet — that is a normal state for a new
// account, not a failure.
func (h *Handler) progressSummary(ctx context.Context, userID uuid.UUID, date time.Time) (string, bool, error) {
	progress, err := h.progress.ForDay(ctx, userID, date)
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	return progress.Summary(), true, nil
}

// mealOptions flattens every plan's meals into "Plan – Meal" options for the
// log-a-meal dropdown. MealPlanService has no "list all meals" query of its
// own, so this loads each plan in full — fine at the size a person's own
// meal plans realistically reach, not a per-message hot path.
func (h *Handler) mealOptions(ctx context.Context, userID uuid.UUID) ([]nutritionpages.MealOption, error) {
	plans, err := h.plans.ListPlans(ctx, userID)
	if err != nil {
		return nil, err
	}

	var opts []nutritionpages.MealOption
	for _, p := range plans {
		full, err := h.plans.GetPlan(ctx, p.ID, userID)
		if err != nil {
			return nil, err
		}
		for _, m := range full.Meals {
			opts = append(opts, nutritionpages.MealOption{ID: m.ID.String(), Label: p.Name + " – " + m.Name})
		}
	}
	return opts, nil
}

func (h *Handler) logIngredient(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	ingredientID, err := uuid.Parse(r.PostFormValue("ingredient_id"))
	if err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}
	quantity := parseFloat(r.PostFormValue("quantity_grams"))

	if _, err := h.foodLog.LogIngredient(r.Context(), user.ID, LogIngredientInput{
		IngredientID: ingredientID, QuantityGrams: quantity, LogDate: time.Now().In(user.Location()),
	}); err != nil {
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/nutrition/log", http.StatusSeeOther)
}

func (h *Handler) logMeal(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	mealID, err := uuid.Parse(r.PostFormValue("meal_id"))
	if err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	if _, err := h.foodLog.LogMeal(r.Context(), user.ID, LogMealInput{MealID: mealID, LogDate: time.Now().In(user.Location())}); err != nil {
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/nutrition/log", http.StatusSeeOther)
}

func (h *Handler) deleteLogEntry(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	if err := h.foodLog.Delete(r.Context(), id, user.ID); err != nil {
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/nutrition/log", http.StatusSeeOther)
}
