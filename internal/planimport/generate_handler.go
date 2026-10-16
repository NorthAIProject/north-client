package planimport

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	pages "github.com/NorthAIProject/north-client/web/planimport"
)

// mealGenerateForm shows the form, filled with the person's last answers, or
// sends them to the calculator when there is no target to build to.
func (h *Handler) mealGenerateForm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.MustUser(ctx)

	form := pages.MealGenerateForm{Objective: "maintenance", Days: 3, MealsPerDay: 4, PlanType: string(meal.MidCarb)}
	active, err := h.svc.mealPlans.ActiveTarget(ctx, user.ID)
	if err != nil {
		h.logFailure(r, "meal plan generate: load target", err)
	}
	form.NoTarget = err == nil && active == nil

	last, err := h.svc.MealIntakeFor(ctx, user.ID)
	if err != nil {
		h.logFailure(r, "meal plan generate: load last intake", err)
	}
	if last != nil {
		form = pages.MealGenerateForm{
			Objective: last.Objective, Days: last.Days, MealsPerDay: last.MealsPerDay,
			PlanType: string(last.PlanType), Exclusions: last.Exclusions, Notes: last.Notes,
			NoTarget: form.NoTarget,
		}
	}
	h.render(w, r, http.StatusOK, pages.MealGeneratePage(user, form))
}

// mealGenerate drafts the plan and shows it on the import's review page, from
// where it is saved exactly as an imported plan is.
func (h *Handler) mealGenerate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.MustUser(ctx)
	if err := r.ParseForm(); err != nil {
		h.render(w, r, http.StatusBadRequest, pages.MealGeneratePage(user, pages.MealGenerateForm{Error: i18n.T(ctx, "planimport.err.invalid")}))
		return
	}

	in := meals.MealPlanIntake{
		Objective:   strings.TrimSpace(r.PostFormValue("objective")),
		Days:        atoi(r.PostFormValue("days")),
		MealsPerDay: atoi(r.PostFormValue("meals_per_day")),
		PlanType:    meal.PlanType(strings.TrimSpace(r.PostFormValue("plan_type"))),
		Exclusions:  meals.ParseExclusions(r.PostFormValue("exclusions")),
		Notes:       strings.TrimSpace(r.PostFormValue("notes")),
	}

	d, err := h.svc.GenerateMeal(ctx, user, in)
	if err == nil {
		h.render(w, r, http.StatusOK, pages.MealReviewPage(user, h.mealView(ctx, d, nil)))
		return
	}

	form := pages.MealGenerateForm{
		Objective: in.Objective, Days: in.Days, MealsPerDay: in.MealsPerDay,
		PlanType: string(in.PlanType), Exclusions: in.Exclusions, Notes: in.Notes,
	}
	var fields apperr.FieldErrors
	switch {
	case apperr.As(meals.Localize(ctx, err), &fields):
		form.Errors = fields.Messages()
		if _, ok := form.Errors["macro_target"]; ok {
			form.NoTarget = true
		}
	case apperr.Is(err, apperr.ErrUnavailable):
		h.logFailure(r, "meal plan generate: no plan fitted", err)
		form.Error = i18n.T(ctx, "planimport.generate.failed")
	default:
		h.logFailure(r, "meal plan generate: failed", err)
		form.Error = message(ctx, err)
	}
	h.render(w, r, httpx.Status(err), pages.MealGeneratePage(user, form))
}

// atoi reads a form number, zero for anything that is not one, which
// validation then names.
func atoi(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}
