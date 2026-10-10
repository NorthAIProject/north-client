package meals

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
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
		var rec Recommendation
		rec, err = h.recommend.Recommend(ctx, user.ID)
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

	mealOptions, err := h.mealOptions(ctx, user.ID, today.Weekday())
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

// mealOptions lists the meals of every plan that can be logged today, for
// the log-a-meal dropdown. MealPlanService has no "list all meals" query of
// its own, so this loads each plan in full — fine at the size a person's own
// meal plans realistically reach, not a per-message hot path.
func (h *Handler) mealOptions(ctx context.Context, userID uuid.UUID, today time.Weekday) ([]nutritionpages.MealOption, error) {
	plans, err := h.plans.ListPlans(ctx, userID)
	if err != nil {
		return nil, err
	}

	full := make([]meal.MealPlan, 0, len(plans))
	for _, p := range plans {
		plan, err := h.plans.GetPlan(ctx, p.ID, userID)
		if err != nil {
			return nil, err
		}
		full = append(full, plan)
	}
	return mealPickerOptions(full, today), nil
}

// mealPickerOptions flattens each plan's meals, every option of every slot,
// into "Plan – Meal · Option" entries. A plan with a day for today offers
// only that day: an every-day plan would otherwise list each meal seven
// times over. A plan without one offers all its days, "Plan – Mon – Meal".
func mealPickerOptions(plans []meal.MealPlan, today time.Weekday) []nutritionpages.MealOption {
	var opts []nutritionpages.MealOption
	for _, p := range plans {
		days, prefixDay := todaysDays(p, today)
		for _, d := range days {
			prefix := p.Name + " – "
			if prefixDay {
				prefix += d.Weekday.String()[:3] + " – "
			}
			for _, slot := range d.Meals {
				for _, m := range slot.Options() {
					opts = append(opts, nutritionpages.MealOption{ID: m.ID.String(), Label: prefix + m.DisplayName()})
				}
			}
		}
	}
	return opts
}

// todaysDays is the plan's day for today when it has one; otherwise every
// day, which then need their weekday to tell them apart.
func todaysDays(p meal.MealPlan, today time.Weekday) (days []meal.Day, prefixDay bool) {
	for _, d := range p.Days {
		if d.Weekday == today {
			return []meal.Day{d}, false
		}
	}
	return p.Days, true
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
