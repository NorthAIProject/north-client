package meals

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
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

	h.render(w, r, http.StatusOK, nutritionpages.PlansIndexPage(user, plans, nutritionpages.PlanForm{}))
}

func (h *Handler) createPlan(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	form := nutritionpages.PlanForm{Name: r.PostFormValue("name"), Description: r.PostFormValue("description")}

	plan, err := h.plans.CreatePlan(r.Context(), user.ID, MealPlanInput{Name: form.Name, Description: form.Description})
	if err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			plans, listErr := h.plans.ListPlans(r.Context(), user.ID)
			if listErr != nil {
				h.fail(w, r, listErr)
				return
			}
			h.render(w, r, http.StatusUnprocessableEntity, nutritionpages.PlansIndexPage(user, plans, form))
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

	h.render(w, r, status, nutritionpages.PlanDetailPage(user, plan, allIngredients, mealForm, ingredientForm))
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
	if _, err := h.plans.AddMeal(r.Context(), planID, user.ID, MealInput{Name: name, MealNumber: len(plan.Meals) + 1}); err != nil {
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
	quantity := parseFloat(r.PostFormValue("quantity_grams"))

	if _, err := h.plans.AddIngredient(r.Context(), mealID, user.ID, MealIngredientInput{IngredientID: ingredientID, QuantityGrams: quantity}); err != nil {
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

// redirectToReferer sends the browser back to the page it came from (the
// plan detail page), falling back to a sane default if the header is
// missing — simpler than threading the plan id through every one of these
// child-resource handlers just to build the same URL.
func redirectToReferer(w http.ResponseWriter, r *http.Request, fallback string) {
	if ref := r.Referer(); ref != "" {
		http.Redirect(w, r, ref, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fallback, http.StatusSeeOther)
}
