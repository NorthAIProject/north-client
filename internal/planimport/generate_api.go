package planimport

import (
	"net/http"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// MealIntakeRequest is what a person asks a meal plan to be built around.
// Objective and planType are strings, not closed enums, so a value added
// later never breaks a shipped client's decoding.
type MealIntakeRequest struct {
	Objective   string   `json:"objective"`
	Days        int      `json:"days"`
	MealsPerDay int      `json:"mealsPerDay"`
	PlanType    string   `json:"planType"`
	Exclusions  []string `json:"exclusions"`
	Notes       string   `json:"notes"`
}

// MealIntakeResponse is the person's last answers, and whether they have a
// macro target to build to. Intake is null when they have never asked.
type MealIntakeResponse struct {
	Intake    *MealIntakeRequest `json:"intake"`
	HasTarget bool               `json:"hasTarget"`
}

func (a *API) mealIntake(c *echo.Context) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	active, err := a.svc.mealPlans.ActiveTarget(ctx, user.ID)
	if err != nil {
		return httpx.Fail(err, i18n.T(ctx, "planimport.api.read"))
	}
	last, err := a.svc.MealIntakeFor(ctx, user.ID)
	if err != nil {
		return httpx.Fail(err, i18n.T(ctx, "planimport.api.read"))
	}
	out := MealIntakeResponse{HasTarget: active != nil}
	if last != nil {
		out.Intake = &MealIntakeRequest{
			Objective: last.Objective, Days: last.Days, MealsPerDay: last.MealsPerDay,
			PlanType: string(last.PlanType), Exclusions: last.Exclusions, Notes: last.Notes,
		}
	}
	return c.JSON(http.StatusOK, out)
}

// generateMeal answers with a draft in the shape an import's parse returns,
// so the client reviews and saves it through the same preview and commit.
func (a *API) generateMeal(c *echo.Context) error {
	ctx := c.Request().Context()
	var req MealIntakeRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{}, i18n.T(ctx, "planimport.api.read")); err != nil {
		return err
	}
	draft, err := a.svc.GenerateMeal(ctx, auth.MustUser(ctx), meals.MealPlanIntake{
		Objective:   req.Objective,
		Days:        req.Days,
		MealsPerDay: req.MealsPerDay,
		PlanType:    meal.PlanType(strings.TrimSpace(req.PlanType)),
		Exclusions:  req.Exclusions,
		Notes:       req.Notes,
	})
	if err != nil {
		if apperr.Is(err, apperr.ErrUnavailable) {
			return httpx.Fail(err, i18n.T(ctx, "planimport.generate.failed"))
		}
		return failWith(c, err, i18n.T(ctx, "planimport.generate.failed"))
	}
	return c.JSON(http.StatusOK, draft)
}
