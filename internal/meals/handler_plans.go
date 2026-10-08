package meals

import (
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	nutritionpages "github.com/NorthAIProject/north-client/web/nutrition"
)

func (h *Handler) plansIndex(w http.ResponseWriter, r *http.Request) {
	h.renderPlansIndex(w, r, http.StatusOK, nutritionpages.PlanForm{})
}

func (h *Handler) renderPlansIndex(w http.ResponseWriter, r *http.Request, status int, form nutritionpages.PlanForm) {
	user := auth.MustUser(r.Context())

	plans, err := h.plans.ListPlans(r.Context(), user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	target, err := h.plans.ActiveTarget(r.Context(), user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	h.render(w, r, status, nutritionpages.PlansIndexPage(user, plans, target, form))
}

func (h *Handler) createPlan(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	if err := r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	form := nutritionpages.PlanForm{
		Name: r.PostFormValue("name"), Description: r.PostFormValue("description"),
		Mode: meal.Mode(r.PostFormValue("mode")), PlanType: meal.PlanType(r.PostFormValue("plan_type")),
		CustomCarbPct: r.PostFormValue("custom_carb_pct"), DayCount: r.PostFormValue("day_count"),
	}
	in := MealPlanInput{
		Name: form.Name, Description: form.Description,
		Settings: meal.PlanSettings{Type: form.PlanType, CustomCarbPct: optionalFloat(form.CustomCarbPct), Mode: form.Mode},
	}
	// The form posts both the easy day count and the advanced weekdays; only
	// the chosen mode's field counts.
	if form.Mode == meal.Advanced {
		for _, v := range r.PostForm["weekdays"] {
			if n, err := strconv.Atoi(v); err == nil {
				form.Weekdays = append(form.Weekdays, time.Weekday(n))
			}
		}
		in.Weekdays = form.Weekdays
	} else {
		in.DayCount, _ = strconv.Atoi(form.DayCount)
	}

	plan, err := h.plans.CreatePlan(r.Context(), user.ID, in, nil, false)
	if err != nil {
		var fieldErrs apperr.FieldErrors
		if apperr.As(err, &fieldErrs) {
			form.Errors = fieldErrs.Messages()
			h.renderPlansIndex(w, r, http.StatusUnprocessableEntity, form)
			return
		}
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, planURL(plan.ID), http.StatusSeeOther)
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
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	h.renderPlan(w, r, http.StatusOK, id, nutritionpages.PlanPage{})
}

// renderPlan renders a plan's page, carrying any refused form in page.
func (h *Handler) renderPlan(w http.ResponseWriter, r *http.Request, status int, planID uuid.UUID, page nutritionpages.PlanPage) {
	user := auth.MustUser(r.Context())

	plan, err := h.plans.GetPlan(r.Context(), planID, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	target, err := h.plans.ActiveTarget(r.Context(), user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	ingredients, err := h.ingredients.Search(r.Context(), user.ID, "", 200)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	page.Plan, page.Target, page.Ingredients = plan, target, ingredients
	h.render(w, r, status, nutritionpages.PlanDetailPage(user, page))
}

// changeFailed shows a refused change on its plan's page: an overage with the
// amounts and, for an advanced plan, a button that sends the same form again
// confirmed; field errors next to the form named by scope.
func (h *Handler) changeFailed(w http.ResponseWriter, r *http.Request, planID uuid.UUID, scope string, err error) {
	var over *OverageError
	var fieldErrs apperr.FieldErrors
	switch {
	case apperr.As(err, &over):
		h.renderPlan(w, r, http.StatusConflict, planID, nutritionpages.PlanPage{Overage: &nutritionpages.OverageNotice{
			Message: over.Error(), CanConfirm: over.Verdict.CanConfirm, Days: over.Verdict.Over, Repost: repostOf(r),
		}})
	case apperr.As(err, &fieldErrs):
		h.renderPlan(w, r, http.StatusUnprocessableEntity, planID, nutritionpages.PlanPage{
			Problem: nutritionpages.Problem{Scope: scope, Errors: fieldErrs.Messages()},
		})
	default:
		h.fail(w, r, err)
	}
}

// repostOf keeps a submitted form, minus its CSRF token and any earlier
// confirmation, so it can be sent again with the overage confirmed.
func repostOf(r *http.Request) nutritionpages.Repost {
	names := make([]string, 0, len(r.PostForm))
	for name := range r.PostForm {
		if name != middleware.CSRFFieldName && name != "confirm_overage" {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	out := nutritionpages.Repost{Action: r.URL.Path}
	for _, name := range names {
		for _, v := range r.PostForm[name] {
			out.Fields = append(out.Fields, nutritionpages.RepostField{Name: name, Value: v})
		}
	}
	return out
}

func (h *Handler) updatePlanSettings(w http.ResponseWriter, r *http.Request) {
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

	err = h.plans.UpdateSettings(r.Context(), planID, user.ID, PlanSettingsInput{
		Name: r.PostFormValue("name"), Description: r.PostFormValue("description"),
		Settings: meal.PlanSettings{
			Type: meal.PlanType(r.PostFormValue("plan_type")), Mode: meal.Mode(r.PostFormValue("mode")),
			CustomCarbPct: optionalFloat(r.PostFormValue("custom_carb_pct")),
		},
		ConfirmReset: r.PostFormValue("confirm_reset") != "", ConfirmOverage: r.PostFormValue("confirm_overage") != "",
	})
	if err != nil {
		h.changeFailed(w, r, planID, "settings", err)
		return
	}

	http.Redirect(w, r, planURL(planID), http.StatusSeeOther)
}

func (h *Handler) addDay(w http.ResponseWriter, r *http.Request) {
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

	var weekday *time.Weekday
	if n, err := strconv.Atoi(r.PostFormValue("weekday")); err == nil {
		wd := time.Weekday(n)
		weekday = &wd
	}
	if _, err := h.plans.AddDay(r.Context(), planID, user.ID, weekday); err != nil {
		h.changeFailed(w, r, planID, "days", err)
		return
	}

	http.Redirect(w, r, planURL(planID), http.StatusSeeOther)
}

func (h *Handler) updateDay(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	dayID, err := uuid.Parse(chi.URLParam(r, "dayID"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if err = r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	o := meal.DayOverride{
		CarbG: optionalFloat(r.PostFormValue("carb_g")), ProteinG: optionalFloat(r.PostFormValue("protein_g")),
		FatG: optionalFloat(r.PostFormValue("fat_g")),
	}
	if v := r.PostFormValue("carb_type"); v != "" {
		t := meal.PlanType(v)
		o.CarbType = &t
	}
	planID, err := h.plans.PlanIDOfDay(r.Context(), dayID, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if err := h.plans.UpdateDay(r.Context(), dayID, user.ID, o, r.PostFormValue("confirm_overage") != ""); err != nil {
		h.changeFailed(w, r, planID, "day:"+dayID.String(), err)
		return
	}

	http.Redirect(w, r, planURL(planID), http.StatusSeeOther)
}

func (h *Handler) removeDay(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	dayID, err := uuid.Parse(chi.URLParam(r, "dayID"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	planID, err := h.plans.PlanIDOfDay(r.Context(), dayID, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if err := h.plans.RemoveDay(r.Context(), dayID, user.ID); err != nil {
		h.changeFailed(w, r, planID, "days", err)
		return
	}

	http.Redirect(w, r, planURL(planID), http.StatusSeeOther)
}

func (h *Handler) addMeal(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	dayID, err := uuid.Parse(chi.URLParam(r, "dayID"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if err = r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	planID, err := h.plans.PlanIDOfDay(r.Context(), dayID, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if _, err := h.plans.AddMeal(r.Context(), dayID, user.ID, r.PostFormValue("name")); err != nil {
		h.changeFailed(w, r, planID, "meal:"+dayID.String(), err)
		return
	}

	http.Redirect(w, r, planURL(planID), http.StatusSeeOther)
}

func (h *Handler) removeMeal(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	mealID, err := uuid.Parse(chi.URLParam(r, "mealID"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	planID, err := h.plans.PlanIDOfMeal(r.Context(), mealID, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if err := h.plans.RemoveMeal(r.Context(), mealID, user.ID); err != nil {
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, planURL(planID), http.StatusSeeOther)
}

func (h *Handler) addIngredientToMeal(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	mealID, err := uuid.Parse(chi.URLParam(r, "mealID"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if err = r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	planID, err := h.plans.PlanIDOfMeal(r.Context(), mealID, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	ingredientID, _ := uuid.Parse(r.PostFormValue("ingredient_id"))
	quantity := parseFloat(r.PostFormValue("quantity_grams"))

	if _, err := h.plans.AddIngredient(r.Context(), mealID, user.ID,
		MealIngredientInput{IngredientID: ingredientID, QuantityGrams: quantity}, r.PostFormValue("confirm_overage") != ""); err != nil {
		h.changeFailed(w, r, planID, "portion:"+mealID.String(), err)
		return
	}

	http.Redirect(w, r, planURL(planID), http.StatusSeeOther)
}

// addIngredientsToMeal takes the reviewed lines of a spoken meal.
//
// Each line posts as ingredient_id_N and quantity_grams_N, and every "line"
// value names an N the person left ticked. Indexed rather than two parallel
// lists because an unticked checkbox posts nothing, which would shift every
// later quantity onto the wrong food.
func (h *Handler) addIngredientsToMeal(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	mealID, err := uuid.Parse(chi.URLParam(r, "mealID"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	if err = r.ParseForm(); err != nil {
		h.fail(w, r, apperr.ErrValidation)
		return
	}

	planID, err := h.plans.PlanIDOfMeal(r.Context(), mealID, user.ID)
	if err != nil {
		h.fail(w, r, err)
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

	if _, err := h.plans.AddIngredients(r.Context(), mealID, user.ID, lines, r.PostFormValue("confirm_overage") != ""); err != nil {
		h.changeFailed(w, r, planID, "portion:"+mealID.String(), err)
		return
	}

	http.Redirect(w, r, planURL(planID), http.StatusSeeOther)
}

func (h *Handler) removeIngredientFromMeal(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}

	planID, err := h.plans.PlanIDOfMealIngredient(r.Context(), id, user.ID)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if err := h.plans.RemoveIngredient(r.Context(), id, user.ID); err != nil {
		h.fail(w, r, err)
		return
	}

	http.Redirect(w, r, planURL(planID), http.StatusSeeOther)
}

// planURL is where every plan change lands. Explicit rather than the Referer:
// a refused change renders the plan at the form's own URL, and going "back"
// there after a confirmed retry would be a GET on a POST-only route.
func planURL(id uuid.UUID) string { return "/app/nutrition/plans/" + id.String() }

// optionalFloat reads a number field the person may leave blank; blank or
// unreadable is nil.
func optionalFloat(s string) *float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return nil
	}
	return &v
}
