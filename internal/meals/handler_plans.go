package meals

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	nutritionpages "github.com/NorthAIProject/north-client/web/nutrition"
)

func (h *Handler) plansIndex(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	plans, err := h.plans.ListPlans(r.Context(), user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	var activeMacro *calculator.MacroPlan
	if h.goals != nil {
		if m, err := h.goals.Current(r.Context(), user.ID); err == nil {
			activeMacro = &m
		}
	}

	h.render(w, r, http.StatusOK, nutritionpages.PlansIndexPage(user, plans, nutritionpages.PlanForm{}, activeMacro))
}

func (h *Handler) createPlan(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	form := nutritionpages.PlanForm{
		Name:          r.PostFormValue("name"),
		Description:   r.PostFormValue("description"),
		PlanType:      r.PostFormValue("plan_type"),
		CustomCarbPct: r.PostFormValue("custom_carb_pct"),
	}

	var customPct *float64
	if form.CustomCarbPct != "" {
		v := parseFloat(form.CustomCarbPct)
		customPct = &v
	}

	var macroPlanID *uuid.UUID
	var activeMacro *calculator.MacroPlan
	if h.goals != nil {
		if cur, err := h.goals.Current(r.Context(), user.ID); err == nil {
			macroPlanID = &cur.ID
			activeMacro = &cur
		}
	}

	plan, err := h.plans.CreatePlan(r.Context(), user.ID, MealPlanInput{
		Name:          form.Name,
		Description:   form.Description,
		PlanType:      form.PlanType,
		CustomCarbPct: customPct,
		MacroPlanID:   macroPlanID,
	})
	if err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			plans, listErr := h.plans.ListPlans(r.Context(), user.ID)
			if listErr != nil {
				h.fail(w, r, listErr)
				return
			}
			h.render(w, r, http.StatusUnprocessableEntity, nutritionpages.PlansIndexPage(user, plans, form, activeMacro))
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/nutrition/plans/"+plan.ID.String(), http.StatusSeeOther)
}

func (h *Handler) deletePlan(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	if err := h.plans.DeletePlan(r.Context(), id, user.ID); err != nil {
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/nutrition/plans", http.StatusSeeOther)
}

func (h *Handler) planDetail(w http.ResponseWriter, r *http.Request) {
	h.renderPlanDetail(w, r, http.StatusOK, nutritionpages.MealForm{}, nutritionpages.MealIngredientForm{})
}

func (h *Handler) renderPlanDetail(w http.ResponseWriter, r *http.Request, status int, mealForm nutritionpages.MealForm, ingredientForm nutritionpages.MealIngredientForm) {
	user := auth.MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	plan, err := h.plans.GetPlan(r.Context(), id, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	allIngredients, err := h.ingredients.Search(r.Context(), user.ID, "", 200)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	var activeMacro *calculator.MacroPlan
	if h.goals != nil {
		if m, err := h.goals.Current(r.Context(), user.ID); err == nil {
			activeMacro = &m
		}
	}

	dayTotals := make(map[int]meal.Macros)
	for _, m := range plan.Meals {
		if m.Weekday != nil {
			dayTotals[*m.Weekday] = dayTotals[*m.Weekday].Add(m.TotalMacros)
		}
	}

	h.render(w, r, status, nutritionpages.PlanDetailPage(user, plan, allIngredients, mealForm, ingredientForm, activeMacro, dayTotals))
}

func (h *Handler) addMeal(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	planID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if err = r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	plan, err := h.plans.GetPlan(r.Context(), planID, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	name := r.PostFormValue("name")
	var weekday *int
	if wStr := r.PostFormValue("weekday"); wStr != "" && wStr != "-1" {
		if v, err := strconv.Atoi(wStr); err == nil {
			weekday = &v
		}
	}

	if _, err := h.plans.AddMeal(r.Context(), planID, user.ID, MealInput{
		Name:       name,
		MealNumber: len(plan.Meals) + 1,
		Weekday:    weekday,
	}); err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			h.renderPlanDetail(w, r, http.StatusUnprocessableEntity, nutritionpages.MealForm{Name: name, Errors: fieldErrs.Messages()}, nutritionpages.MealIngredientForm{})
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, "/app/nutrition/plans/"+planID.String(), http.StatusSeeOther)
}

func (h *Handler) updateMealDay(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	mealID, err := uuid.Parse(chi.URLParam(r, "mealID"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	mealRow, err := h.plans.repo.GetMeal(r.Context(), mealID, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	var weekday *int
	if wStr := r.PostFormValue("weekday"); wStr != "" && wStr != "-1" {
		if v, err := strconv.Atoi(wStr); err == nil {
			weekday = &v
		}
	}
	dayPlanType := r.PostFormValue("day_plan_type")

	if _, err := h.plans.UpdateMealDay(r.Context(), mealID, user.ID, MealInput{
		Name:        mealRow.Name,
		MealNumber:  mealRow.MealNumber,
		Weekday:     weekday,
		DayPlanType: dayPlanType,
	}); err != nil {
		h.fail(w, r, err)
		return
	}

	redirectToReferer(w, r, "/app/nutrition/plans")
}

func (h *Handler) removeMeal(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	mealID, err := uuid.Parse(chi.URLParam(r, "mealID"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	if err := h.plans.RemoveMeal(r.Context(), mealID, user.ID); err != nil {
		h.fail(w, r, err)
		return
	}

	redirectToReferer(w, r, "/app/nutrition/plans")
}

func (h *Handler) addIngredientToMeal(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	mealID, err := uuid.Parse(chi.URLParam(r, "mealID"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	ingredientID, _ := uuid.Parse(r.PostFormValue("ingredient_id"))
	quantityStr := r.PostFormValue("quantity_grams")
	quantity := parseFloat(quantityStr)
	confirmOverage := r.PostFormValue("confirm_overage") == "1" || r.PostFormValue("confirm_overage") == "true"

	_, overage, err := h.plans.AddIngredientChecked(r.Context(), mealID, user.ID, MealIngredientInput{
		IngredientID:  ingredientID,
		QuantityGrams: quantity,
	}, confirmOverage, h.goals)
	if err != nil {
		if overage != nil && overage.IsOver {
			mealRow, mealErr := h.plans.repo.GetMeal(r.Context(), mealID, user.ID)
			if mealErr == nil {
				rctx := chi.RouteContext(r.Context())
				if rctx == nil {
					rctx = chi.NewRouteContext()
					r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
				}
				rctx.URLParams.Add("id", mealRow.MealPlanID.String())

				h.renderPlanDetail(w, r, http.StatusUnprocessableEntity, nutritionpages.MealForm{}, nutritionpages.MealIngredientForm{
					MealID:       mealID.String(),
					IngredientID: ingredientID.String(),
					Quantity:     quantityStr,
					Overage:      overage,
				})
				return
			}
		}
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			mealRow, mealErr := h.plans.repo.GetMeal(r.Context(), mealID, user.ID)
			if mealErr == nil {
				rctx := chi.RouteContext(r.Context())
				if rctx == nil {
					rctx = chi.NewRouteContext()
					r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
				}
				rctx.URLParams.Add("id", mealRow.MealPlanID.String())

				h.renderPlanDetail(w, r, http.StatusUnprocessableEntity, nutritionpages.MealForm{}, nutritionpages.MealIngredientForm{
					MealID:       mealID.String(),
					IngredientID: ingredientID.String(),
					Quantity:     quantityStr,
					Errors:       fieldErrs.Messages(),
				})
				return
			}
		}
		h.fail(w, r, err)
		return
	}

	redirectToReferer(w, r, "/app/nutrition/plans")
}

// addIngredientsToMeal takes the reviewed lines of a spoken meal.
func (h *Handler) addIngredientsToMeal(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	mealID, err := uuid.Parse(chi.URLParam(r, "mealID"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	lines := make([]MealIngredientInput, 0, len(r.PostForm["line"]))
	for _, n := range r.PostForm["line"] {
		ingredientID, _ := uuid.Parse(r.PostFormValue("ingredient_id_" + n))
		lines = append(lines, MealIngredientInput{
			IngredientID:  ingredientID,
			QuantityGrams: parseFloat(r.PostFormValue("quantity_grams_" + n)),
		})
	}

	if _, err := h.plans.AddIngredients(r.Context(), mealID, user.ID, lines); err != nil {
		h.fail(w, r, err)
		return
	}

	redirectToReferer(w, r, "/app/nutrition/plans")
}

func (h *Handler) removeIngredientFromMeal(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	if err := h.plans.RemoveIngredient(r.Context(), id, user.ID); err != nil {
		h.fail(w, r, err)
		return
	}

	redirectToReferer(w, r, "/app/nutrition/plans")
}

func redirectToReferer(w http.ResponseWriter, r *http.Request, fallback string) {
	if ref := r.Referer(); ref != "" {
		http.Redirect(w, r, ref, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fallback, http.StatusSeeOther)
}
