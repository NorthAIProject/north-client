package meals

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// API is nutrition for native clients: the web /nutrition pages' ingredient
// library, meal plans and food log, over the same services.
type API struct {
	ingredients *IngredientService
	plans       *MealPlanService
	foodLog     *FoodLogService
	progress    *TrackMealProgressService
	recommend   *GoalRecommendationService
}

// HandlerOptions are the nutrition services the API reads through.
type HandlerOptions struct {
	Ingredients *IngredientService
	Diets       *DietPreferenceService
	Plans       *MealPlanService
	FoodLog     *FoodLogService
	Progress    *TrackMealProgressService
	Recommend   *GoalRecommendationService
}

// NewAPI builds the routes from the web handler's options; register them
// behind auth.Bearer.
func NewAPI(opts HandlerOptions) *API {
	return &API{
		ingredients: opts.Ingredients, plans: opts.Plans, foodLog: opts.FoodLog,
		progress: opts.Progress, recommend: opts.Recommend,
	}
}

func (a *API) Register(g *echo.Group) {
	g.GET("/nutrition/ingredients", a.searchIngredients)
	g.POST("/nutrition/ingredients", a.createIngredient)
	g.DELETE("/nutrition/ingredients/:ingredientID", a.deleteIngredient)

	g.GET("/nutrition/plan-options", a.planOptions)
	g.GET("/nutrition/plans", a.listPlans)
	g.POST("/nutrition/plans", a.createPlan)
	g.GET("/nutrition/plans/:planID", a.showPlan)
	g.PUT("/nutrition/plans/:planID", a.updatePlan)
	g.DELETE("/nutrition/plans/:planID", a.deletePlan)
	g.POST("/nutrition/plans/:planID/days", a.addDay)
	g.PUT("/nutrition/plan-days/:dayID", a.updateDay)
	g.DELETE("/nutrition/plan-days/:dayID", a.removeDay)
	g.POST("/nutrition/plan-days/:dayID/meals", a.addMeal)
	g.DELETE("/nutrition/meals/:mealID", a.removeMeal)
	g.POST("/nutrition/meals/:mealID/options", a.addOption)
	g.PUT("/nutrition/meals/:mealID/optional", a.setMealOptional)
	g.POST("/nutrition/meals/:mealID/ingredients", a.addMealIngredient)
	g.POST("/nutrition/meals/:mealID/ingredients/batch", a.addMealIngredients)
	g.DELETE("/nutrition/meal-ingredients/:mealIngredientID", a.removeMealIngredient)
	g.PUT("/nutrition/meal-ingredients/:mealIngredientID/optional", a.setMealIngredientOptional)

	g.GET("/nutrition/log", a.showLog)
	g.GET("/nutrition/log/meal-options", a.logMealOptions)
	g.POST("/nutrition/log/ingredients", a.logIngredient)
	g.POST("/nutrition/log/meals", a.logMeal)
	g.DELETE("/nutrition/log/:entryID", a.deleteLogEntry)
}

type MacrosView struct {
	Calories float64 `json:"calories"`
	ProteinG float64 `json:"proteinG"`
	FatG     float64 `json:"fatG"`
	CarbG    float64 `json:"carbG"`
}

type IngredientView struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Brand    string    `json:"brand,omitempty"`
	Category string    `json:"category,omitempty"`
	// Own is true for ingredients this person added; the rest are the shared
	// library.
	Own              bool       `json:"own"`
	ServingSizeGrams float64    `json:"servingSizeGrams"`
	Per100g          MacrosView `json:"per100g"`
}

type IngredientList struct {
	Ingredients []IngredientView `json:"ingredients"`
}

type IngredientRequest struct {
	Name             string     `json:"name"`
	Brand            string     `json:"brand"`
	Category         string     `json:"category"`
	ServingSizeGrams float64    `json:"servingSizeGrams"`
	Per100g          MacrosView `json:"per100g"`
}

type MealIngredientView struct {
	ID            uuid.UUID  `json:"id"`
	IngredientID  uuid.UUID  `json:"ingredientId"`
	Name          string     `json:"name"`
	QuantityGrams float64    `json:"quantityGrams"`
	Macros        MacrosView `json:"macros"`
	// SourceText is the line an imported plan had for this food; Estimated
	// marks a food and quantity the importer guessed at.
	SourceText string `json:"sourceText,omitempty"`
	Estimated  bool   `json:"estimated,omitempty"`
	// Optional is a food the plan offers but does not count: its macros are
	// its own and left out of the meal's total.
	Optional bool `json:"optional,omitempty"`
}

// MealView is a meal slot's default option, with the slot's other options.
// Only the default counts toward the day's status, and not even that when the
// slot is optional.
type MealView struct {
	ID          uuid.UUID `json:"id"`
	MealNumber  int       `json:"mealNumber"`
	Name        string    `json:"name"`
	OptionLabel string    `json:"optionLabel,omitempty"`
	// Optional is a meal the plan offers but does not count: its total is
	// its own and left out of the day's status. It covers every option.
	Optional     bool                 `json:"optional"`
	TotalMacros  MacrosView           `json:"totalMacros"`
	Ingredients  []MealIngredientView `json:"ingredients"`
	Alternatives []MealOptionView     `json:"alternatives,omitempty"`
}

// MealOptionView is an alternative option of a meal slot: eaten instead of
// the default, under the slot's name.
type MealOptionView struct {
	ID          uuid.UUID            `json:"id"`
	OptionLabel string               `json:"optionLabel"`
	TotalMacros MacrosView           `json:"totalMacros"`
	Ingredients []MealIngredientView `json:"ingredients"`
}

// DayStatusView is a day measured against its target. Remaining is negative
// where the day is over; Over is how far, zero where it is not.
type DayStatusView struct {
	Target    MacrosView `json:"target"`
	Consumed  MacrosView `json:"consumed"`
	Remaining MacrosView `json:"remaining"`
	Over      MacrosView `json:"over"`
	IsOver    bool       `json:"isOver"`
}

type PlanDayView struct {
	ID uuid.UUID `json:"id"`
	// Weekday is 0 for Sunday through 6 for Saturday.
	Weekday int `json:"weekday"`
	// The day's advanced-mode overrides; absent ones follow the plan.
	CarbType *meal.PlanType `json:"carbType,omitempty"`
	CarbG    *float64       `json:"carbG,omitempty"`
	ProteinG *float64       `json:"proteinG,omitempty"`
	FatG     *float64       `json:"fatG,omitempty"`
	// Status is absent until the person has a macro target.
	Status *DayStatusView `json:"status,omitempty"`
	Meals  []MealView     `json:"meals"`
}

type PlanSummary struct {
	ID            uuid.UUID     `json:"id"`
	Name          string        `json:"name"`
	Description   string        `json:"description"`
	PlanType      meal.PlanType `json:"planType"`
	CustomCarbPct *float64      `json:"customCarbPct,omitempty"`
	Mode          meal.Mode     `json:"mode"`
	DayCount      int           `json:"dayCount"`
	TotalMacros   MacrosView    `json:"totalMacros"`
}

type PlanList struct {
	Plans []PlanSummary `json:"plans"`
}

type PlanDetail struct {
	PlanSummary
	Objective     string `json:"objective"`
	ActivityLevel string `json:"activityLevel"`
	Gender        string `json:"gender"`
	// Notes is free text an imported plan carried beside its meals.
	Notes string `json:"notes,omitempty"`
	// Target is the person's current macro target, absent until the
	// calculator has produced one.
	Target *MacrosView   `json:"target,omitempty"`
	Days   []PlanDayView `json:"days"`
}

// PlanTypeOption is one plan type as the create and edit forms offer it.
type PlanTypeOption struct {
	ID     meal.PlanType `json:"id"`
	MinPct float64       `json:"minPct"`
	MaxPct float64       `json:"maxPct"`
	// DefaultPct and DefaultCarbG are a preset's midpoint; custom has none.
	// DefaultCarbG is absent until the person has a macro target.
	DefaultPct   *float64 `json:"defaultPct,omitempty"`
	DefaultCarbG *float64 `json:"defaultCarbG,omitempty"`
	AdvancedOnly bool     `json:"advancedOnly"`
}

// PlanOptions is what a client needs to offer a new plan: the active target
// and the plan types with their carb shares.
type PlanOptions struct {
	Target    *MacrosView      `json:"target,omitempty"`
	MaxDays   int              `json:"maxDays"`
	PlanTypes []PlanTypeOption `json:"planTypes"`
}

type PlanRequest struct {
	Name          string        `json:"name"`
	Description   string        `json:"description"`
	Objective     string        `json:"objective"`
	ActivityLevel string        `json:"activityLevel"`
	Gender        string        `json:"gender"`
	PlanType      meal.PlanType `json:"planType"`
	CustomCarbPct *float64      `json:"customCarbPct"`
	Mode          meal.Mode     `json:"mode"`
	DayCount      int           `json:"dayCount"`
	Weekdays      []int         `json:"weekdays"`
}

type PlanSettingsRequest struct {
	Name           string        `json:"name"`
	Description    string        `json:"description"`
	PlanType       meal.PlanType `json:"planType"`
	CustomCarbPct  *float64      `json:"customCarbPct"`
	Mode           meal.Mode     `json:"mode"`
	ConfirmReset   bool          `json:"confirmReset"`
	ConfirmOverage bool          `json:"confirmOverage"`
}

type DayRequest struct {
	Weekday *int `json:"weekday"`
}

type DayOverrideRequest struct {
	CarbType       *meal.PlanType `json:"carbType"`
	CarbG          *float64       `json:"carbG"`
	ProteinG       *float64       `json:"proteinG"`
	FatG           *float64       `json:"fatG"`
	ConfirmOverage bool           `json:"confirmOverage"`
}

type MealRequest struct {
	Name string `json:"name"`
}

// OptionRequest adds an option to a meal. An empty label becomes the first
// "Option N" the meal does not use.
type OptionRequest struct {
	Label string `json:"label"`
}

type PortionRequest struct {
	IngredientID   uuid.UUID `json:"ingredientId"`
	QuantityGrams  float64   `json:"quantityGrams"`
	Optional       bool      `json:"optional"`
	ConfirmOverage bool      `json:"confirmOverage"`
}

// OptionalRequest marks a meal, or a meal's food, optional (shown, not
// counted) or counted.
type OptionalRequest struct {
	Optional       bool `json:"optional"`
	ConfirmOverage bool `json:"confirmOverage"`
}

type PortionLine struct {
	IngredientID  uuid.UUID `json:"ingredientId"`
	QuantityGrams float64   `json:"quantityGrams"`
}

type PortionsRequest struct {
	Portions       []PortionLine `json:"portions"`
	ConfirmOverage bool          `json:"confirmOverage"`
}

type MealPortionList struct {
	Portions []MealIngredientView `json:"portions"`
}

// DayOverageView is one day a change would take further over its target.
type DayOverageView struct {
	DayID    uuid.UUID  `json:"dayId"`
	Weekday  int        `json:"weekday"`
	Target   MacrosView `json:"target"`
	Consumed MacrosView `json:"consumed"`
	Over     MacrosView `json:"over"`
}

// MacroOverage is the 409 a change gets when it would take a day further over
// its target. CanConfirm is set for advanced plans, which save the same
// request once it carries confirmOverage; easy plans never do.
type MacroOverage struct {
	Message    string           `json:"message"`
	CanConfirm bool             `json:"canConfirm"`
	Days       []DayOverageView `json:"days"`
}

type LogMealRequest struct {
	MealID uuid.UUID `json:"mealId"`
}

// LoggableMeal is one entry of the web log page's "log a meal" picker: a
// plan's meal option flattened to "Plan – Meal · Option".
type LoggableMeal struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type LoggableMealList struct {
	Options []LoggableMeal `json:"options"`
}

type FoodLogEntryView struct {
	ID            uuid.UUID  `json:"id"`
	Label         string     `json:"label"`
	QuantityGrams *float64   `json:"quantityGrams,omitempty"`
	Macros        MacrosView `json:"macros"`
	LoggedAt      time.Time  `json:"loggedAt"`
}

type NutritionProgress struct {
	Goal   MacrosView `json:"goal"`
	Logged MacrosView `json:"logged"`
	// Summary is the web's line, e.g. "420 kcal under your goal".
	Summary string `json:"summary"`
	// Recommendation is the coach's suggestion for the goal itself.
	Recommendation string `json:"recommendation,omitempty"`
}

type FoodLog struct {
	// Date is today in the person's zone.
	Date    string             `json:"date"`
	Entries []FoodLogEntryView `json:"entries"`
	Totals  MacrosView         `json:"totals"`
	// Progress is absent until a macro goal exists.
	Progress *NutritionProgress `json:"progress,omitempty"`
}

// searchIngredients takes an optional limit (1–200, default 60): the web
// pages list 50 on the library page and up to 200 in their pickers.
func (a *API) searchIngredients(c *echo.Context) error {
	ctx := c.Request().Context()
	userID := auth.MustUser(ctx).ID
	limit := 60
	if n, err := strconv.Atoi(c.QueryParam("limit")); err == nil && n >= 1 {
		limit = min(n, 200)
	}
	list, err := a.ingredients.Search(ctx, userID, c.QueryParam("q"), limit)
	if err != nil {
		return httpx.Fail(localize(ctx, err), i18n.T(ctx, "nutrition.api.ingredients.load"))
	}
	out := IngredientList{Ingredients: make([]IngredientView, 0, len(list))}
	for _, in := range list {
		out.Ingredients = append(out.Ingredients, projectIngredient(in, userID))
	}
	return c.JSON(http.StatusOK, out)
}

func (a *API) createIngredient(c *echo.Context) error {
	ctx := c.Request().Context()
	var req IngredientRequest
	if err := readJSON(c, &req); err != nil {
		return err
	}
	userID := auth.MustUser(ctx).ID
	in, err := a.ingredients.Create(ctx, userID, IngredientInput{
		Name: req.Name, Brand: req.Brand, Category: req.Category, ServingSizeGrams: req.ServingSizeGrams,
		Per100g: Macros(req.Per100g),
	})
	if err != nil {
		return httpx.Fail(localize(ctx, err), i18n.T(ctx, "nutrition.api.ingredient.save"))
	}
	return c.JSON(http.StatusCreated, projectIngredient(in, userID))
}

func (a *API) deleteIngredient(c *echo.Context) error {
	return a.remove(c, "ingredientID", a.ingredients.Delete, "nutrition.api.ingredient.delete")
}

func (a *API) planOptions(c *echo.Context) error {
	ctx := c.Request().Context()
	target, err := a.plans.ActiveTarget(ctx, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(localize(ctx, err), i18n.T(ctx, "nutrition.api.options.load"))
	}
	out := PlanOptions{Target: macrosViewPtr(target), MaxDays: meal.MaxDays}
	for _, b := range meal.CarbBands {
		pct := b.DefaultPct()
		opt := PlanTypeOption{ID: b.Type, MinPct: b.MinPct, MaxPct: b.MaxPct, DefaultPct: &pct}
		if target != nil {
			grams := b.CarbG(target.CarbG)
			opt.DefaultCarbG = &grams
		}
		out.PlanTypes = append(out.PlanTypes, opt)
	}
	out.PlanTypes = append(out.PlanTypes, PlanTypeOption{
		ID: meal.Custom, MinPct: 0, MaxPct: 100, AdvancedOnly: true,
	})
	return c.JSON(http.StatusOK, out)
}

func (a *API) listPlans(c *echo.Context) error {
	ctx := c.Request().Context()
	plans, err := a.plans.ListPlans(ctx, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(localize(ctx, err), i18n.T(ctx, "nutrition.api.plans.load"))
	}
	out := PlanList{Plans: make([]PlanSummary, 0, len(plans))}
	for _, p := range plans {
		out.Plans = append(out.Plans, summarizePlan(p))
	}
	return c.JSON(http.StatusOK, out)
}

func (a *API) createPlan(c *echo.Context) error {
	ctx := c.Request().Context()
	var req PlanRequest
	if err := readJSON(c, &req); err != nil {
		return err
	}
	weekdays := make([]time.Weekday, len(req.Weekdays))
	for i, wd := range req.Weekdays {
		weekdays[i] = time.Weekday(wd)
	}
	plan, err := a.plans.CreatePlan(ctx, auth.MustUser(ctx).ID, MealPlanInput{
		Name: req.Name, Description: req.Description, Objective: req.Objective,
		ActivityLevel: req.ActivityLevel, Gender: req.Gender,
		Settings: meal.PlanSettings{Type: req.PlanType, CustomCarbPct: req.CustomCarbPct, Mode: req.Mode},
		DayCount: req.DayCount, Weekdays: weekdays,
	}, nil, false)
	if err != nil {
		return httpx.Fail(localize(ctx, err), i18n.T(ctx, "nutrition.api.plan.save"))
	}
	return a.writePlan(c, http.StatusCreated, plan.ID)
}

func (a *API) showPlan(c *echo.Context) error {
	id, err := pathID(c, "planID")
	if err != nil {
		return err
	}
	return a.writePlan(c, http.StatusOK, id)
}

func (a *API) updatePlan(c *echo.Context) error {
	ctx := c.Request().Context()
	id, err := pathID(c, "planID")
	if err != nil {
		return err
	}
	var req PlanSettingsRequest
	if err = readJSON(c, &req); err != nil {
		return err
	}
	err = a.plans.UpdateSettings(ctx, id, auth.MustUser(ctx).ID, PlanSettingsInput{
		Name: req.Name, Description: req.Description,
		Settings:     meal.PlanSettings{Type: req.PlanType, CustomCarbPct: req.CustomCarbPct, Mode: req.Mode},
		ConfirmReset: req.ConfirmReset, ConfirmOverage: req.ConfirmOverage,
	})
	if err != nil {
		return writeChangeError(c, err, i18n.T(ctx, "nutrition.api.plan.save"))
	}
	return a.writePlan(c, http.StatusOK, id)
}

func (a *API) deletePlan(c *echo.Context) error {
	return a.remove(c, "planID", a.plans.DeletePlan, "nutrition.api.plan.delete")
}

func (a *API) addDay(c *echo.Context) error {
	ctx := c.Request().Context()
	id, err := pathID(c, "planID")
	if err != nil {
		return err
	}
	var req DayRequest
	if err := readJSON(c, &req); err != nil {
		return err
	}
	var weekday *time.Weekday
	if req.Weekday != nil {
		wd := time.Weekday(*req.Weekday)
		weekday = &wd
	}
	if _, err := a.plans.AddDay(ctx, id, auth.MustUser(ctx).ID, weekday); err != nil {
		return httpx.Fail(localize(ctx, err), i18n.T(ctx, "nutrition.api.day.add"))
	}
	return a.writePlan(c, http.StatusCreated, id)
}

func (a *API) updateDay(c *echo.Context) error {
	ctx := c.Request().Context()
	id, err := pathID(c, "dayID")
	if err != nil {
		return err
	}
	var req DayOverrideRequest
	if err = readJSON(c, &req); err != nil {
		return err
	}
	userID := auth.MustUser(ctx).ID
	planID, err := a.plans.PlanIDOfDay(ctx, id, userID)
	if err != nil {
		return httpx.Fail(localize(ctx, err), i18n.T(ctx, "nutrition.api.day.save"))
	}
	err = a.plans.UpdateDay(ctx, id, userID, meal.DayOverride{
		CarbType: req.CarbType, CarbG: req.CarbG, ProteinG: req.ProteinG, FatG: req.FatG,
	}, req.ConfirmOverage)
	if err != nil {
		return writeChangeError(c, err, i18n.T(ctx, "nutrition.api.day.save"))
	}
	return a.writePlan(c, http.StatusOK, planID)
}

func (a *API) removeDay(c *echo.Context) error {
	return a.remove(c, "dayID", a.plans.RemoveDay, "nutrition.api.day.remove")
}

func (a *API) addMeal(c *echo.Context) error {
	ctx := c.Request().Context()
	id, err := pathID(c, "dayID")
	if err != nil {
		return err
	}
	var req MealRequest
	if err = readJSON(c, &req); err != nil {
		return err
	}
	added, err := a.plans.AddMeal(ctx, id, auth.MustUser(ctx).ID, req.Name)
	if err != nil {
		return httpx.Fail(localize(ctx, err), i18n.T(ctx, "nutrition.api.meal.add"))
	}
	return a.writePlan(c, http.StatusCreated, added.MealPlanID)
}

// addOption adds an empty option to the meal mealID — any of its options —
// and answers with the whole plan, as adding a meal does, so a client
// refreshes in one call.
func (a *API) addOption(c *echo.Context) error {
	ctx := c.Request().Context()
	id, err := pathID(c, "mealID")
	if err != nil {
		return err
	}
	var req OptionRequest
	if err = readJSON(c, &req); err != nil {
		return err
	}
	userID := auth.MustUser(ctx).ID
	planID, err := a.plans.PlanIDOfMeal(ctx, id, userID)
	if err != nil {
		return httpx.Fail(localize(ctx, err), i18n.T(ctx, "nutrition.api.option.add"))
	}
	if _, err = a.plans.AddOption(ctx, userID, planID, id, req.Label); err != nil {
		return httpx.Fail(localize(ctx, err), i18n.T(ctx, "nutrition.api.option.add"))
	}
	return a.writePlan(c, http.StatusCreated, planID)
}

func (a *API) removeMeal(c *echo.Context) error {
	return a.remove(c, "mealID", a.plans.RemoveMeal, "nutrition.api.meal.remove")
}

func (a *API) addMealIngredient(c *echo.Context) error {
	ctx := c.Request().Context()
	id, err := pathID(c, "mealID")
	if err != nil {
		return err
	}
	var req PortionRequest
	if err = readJSON(c, &req); err != nil {
		return err
	}
	added, err := a.plans.AddIngredient(ctx, id, auth.MustUser(ctx).ID,
		MealIngredientInput{IngredientID: req.IngredientID, QuantityGrams: req.QuantityGrams, Optional: req.Optional}, req.ConfirmOverage)
	if err != nil {
		return writeChangeError(c, err, i18n.T(ctx, "nutrition.api.ingredient.add"))
	}
	return c.JSON(http.StatusCreated, projectMealIngredient(added))
}

func (a *API) addMealIngredients(c *echo.Context) error {
	ctx := c.Request().Context()
	id, err := pathID(c, "mealID")
	if err != nil {
		return err
	}
	var req PortionsRequest
	if err = readJSON(c, &req); err != nil {
		return err
	}
	lines := make([]MealIngredientInput, len(req.Portions))
	for i, p := range req.Portions {
		lines[i] = MealIngredientInput{IngredientID: p.IngredientID, QuantityGrams: p.QuantityGrams}
	}
	added, err := a.plans.AddIngredients(ctx, id, auth.MustUser(ctx).ID, lines, req.ConfirmOverage)
	if err != nil {
		return writeChangeError(c, err, i18n.T(ctx, "nutrition.api.ingredients.add"))
	}
	out := MealPortionList{Portions: make([]MealIngredientView, 0, len(added))}
	for _, mi := range added {
		out.Portions = append(out.Portions, projectMealIngredient(mi))
	}
	return c.JSON(http.StatusCreated, out)
}

// setMealIngredientOptional marks one of a meal's foods optional or counted,
// answering with the whole plan so a client refreshes its totals in one call.
func (a *API) setMealIngredientOptional(c *echo.Context) error {
	ctx := c.Request().Context()
	id, err := pathID(c, "mealIngredientID")
	if err != nil {
		return err
	}
	var req OptionalRequest
	if err = readJSON(c, &req); err != nil {
		return err
	}
	userID := auth.MustUser(ctx).ID
	planID, err := a.plans.PlanIDOfMealIngredient(ctx, id, userID)
	if err != nil {
		return httpx.Fail(localize(ctx, err), i18n.T(ctx, "nutrition.api.ingredient.change"))
	}
	if _, err := a.plans.SetIngredientOptional(ctx, id, userID, req.Optional, req.ConfirmOverage); err != nil {
		return writeChangeError(c, err, i18n.T(ctx, "nutrition.api.ingredient.change"))
	}
	return a.writePlan(c, http.StatusOK, planID)
}

// setMealOptional marks a meal slot, every option of it, optional or counted,
// answering with the whole plan so a client refreshes its totals in one call.
func (a *API) setMealOptional(c *echo.Context) error {
	ctx := c.Request().Context()
	id, err := pathID(c, "mealID")
	if err != nil {
		return err
	}
	var req OptionalRequest
	if err = readJSON(c, &req); err != nil {
		return err
	}
	userID := auth.MustUser(ctx).ID
	plan, err := a.plans.SetMealOptional(ctx, id, userID, req.Optional, req.ConfirmOverage)
	if err != nil {
		return writeChangeError(c, err, i18n.T(ctx, "nutrition.api.meal.change"))
	}
	return a.writePlan(c, http.StatusOK, plan.ID)
}

func (a *API) removeMealIngredient(c *echo.Context) error {
	return a.remove(c, "mealIngredientID", a.plans.RemoveIngredient, "nutrition.api.ingredient.remove")
}

func (a *API) showLog(c *echo.Context) error {
	return a.writeLog(c, http.StatusOK)
}

// logMealOptions lists the meals that can be logged today, as the web log
// page's picker offers them.
func (a *API) logMealOptions(c *echo.Context) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	opts, err := mealOptionsFor(ctx, a.plans, user.ID, time.Now().In(user.Location()).Weekday())
	if err != nil {
		return httpx.Fail(localize(ctx, err), i18n.T(ctx, "nutrition.api.plans.load"))
	}
	if opts == nil {
		opts = []LoggableMeal{}
	}
	return c.JSON(http.StatusOK, LoggableMealList{Options: opts})
}

func (a *API) logIngredient(c *echo.Context) error {
	ctx := c.Request().Context()
	var req PortionRequest
	if err := readJSON(c, &req); err != nil {
		return err
	}
	user := auth.MustUser(ctx)
	if _, err := a.foodLog.LogIngredient(ctx, user.ID, LogIngredientInput{
		IngredientID: req.IngredientID, QuantityGrams: req.QuantityGrams, LogDate: time.Now().In(user.Location()),
	}); err != nil {
		return httpx.Fail(localize(ctx, err), i18n.T(ctx, "nutrition.api.log"))
	}
	return a.writeLog(c, http.StatusCreated)
}

func (a *API) logMeal(c *echo.Context) error {
	ctx := c.Request().Context()
	var req LogMealRequest
	if err := readJSON(c, &req); err != nil {
		return err
	}
	user := auth.MustUser(ctx)
	if _, err := a.foodLog.LogMeal(ctx, user.ID, LogMealInput{MealID: req.MealID, LogDate: time.Now().In(user.Location())}); err != nil {
		return httpx.Fail(localize(ctx, err), i18n.T(ctx, "nutrition.api.meal.log"))
	}
	return a.writeLog(c, http.StatusCreated)
}

func (a *API) deleteLogEntry(c *echo.Context) error {
	ctx := c.Request().Context()
	id, err := pathID(c, "entryID")
	if err != nil {
		return err
	}
	if err := a.foodLog.Delete(ctx, id, auth.MustUser(ctx).ID); err != nil {
		return httpx.Fail(localize(ctx, err), i18n.T(ctx, "nutrition.api.entry.delete"))
	}
	return a.writeLog(c, http.StatusOK)
}

// writeLog answers with today's log as the web page shows it: the entries,
// the totals, and progress against the macro goal once there is one.
func (a *API) writeLog(c *echo.Context, status int) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	today := time.Now().In(user.Location())
	entries, err := a.foodLog.Day(ctx, user.ID, today)
	if err != nil {
		return httpx.Fail(localize(ctx, err), i18n.T(ctx, "nutrition.api.log.load"))
	}
	out := FoodLog{Date: today.Format("2006-01-02"), Entries: make([]FoodLogEntryView, 0, len(entries))}
	var totals Macros
	for _, e := range entries {
		totals = totals.Add(e.Macros)
		out.Entries = append(out.Entries, FoodLogEntryView{
			ID: e.ID, Label: e.Label, QuantityGrams: e.QuantityGrams,
			Macros: MacrosView(e.Macros), LoggedAt: e.LoggedAt,
		})
	}
	out.Totals = MacrosView(totals)

	progress, err := a.progress.ForDay(ctx, user.ID, today)
	switch {
	case err == nil:
		p := &NutritionProgress{Goal: MacrosView(progress.Goal), Logged: MacrosView(progress.Logged), Summary: progress.SummaryIn(ctx)}
		if rec, recErr := a.recommend.Recommend(ctx, user.ID); recErr == nil {
			p.Recommendation = i18n.T(ctx, rec.Key)
		} else if !apperr.Is(recErr, apperr.ErrNotFound) {
			return httpx.Fail(localize(ctx, recErr), i18n.T(ctx, "nutrition.api.log.load"))
		}
		out.Progress = p
	case !apperr.Is(err, apperr.ErrNotFound):
		return httpx.Fail(localize(ctx, err), i18n.T(ctx, "nutrition.api.log.load"))
	}
	return c.JSON(status, out)
}

func (a *API) writePlan(c *echo.Context, status int, id uuid.UUID) error {
	ctx := c.Request().Context()
	userID := auth.MustUser(ctx).ID
	plan, err := a.plans.GetPlan(ctx, id, userID)
	if err != nil {
		return httpx.Fail(localize(ctx, err), i18n.T(ctx, "nutrition.api.plan.load"))
	}
	target, err := a.plans.ActiveTarget(ctx, userID)
	if err != nil {
		return httpx.Fail(localize(ctx, err), i18n.T(ctx, "nutrition.api.plan.load"))
	}
	return c.JSON(status, projectPlan(plan, target))
}

// writeChangeError answers a refused change: a 409 MacroOverage when it would
// take a day over its target, the usual error body otherwise. It returns the
// error for the usual body and nil once the 409 is written.
func writeChangeError(c *echo.Context, err error, message string) error {
	ctx := c.Request().Context()
	var over *OverageError
	if !errors.As(err, &over) {
		return httpx.Fail(localize(ctx, err), message)
	}
	return c.JSON(http.StatusConflict, ProjectOverage(ctx, over))
}

// ProjectOverage is the 409 body for a refused change, its message in ctx's
// language. Exported so plan import answers an over-target plan in the same
// shape as the plan pages.
func ProjectOverage(ctx context.Context, over *OverageError) MacroOverage {
	out := MacroOverage{Message: over.MessageIn(ctx), CanConfirm: over.Verdict.CanConfirm, Days: make([]DayOverageView, 0, len(over.Verdict.Over))}
	for _, d := range over.Verdict.Over {
		out.Days = append(out.Days, DayOverageView{
			DayID: d.DayID, Weekday: int(d.Weekday),
			Target: MacrosView(d.Status.Target), Consumed: MacrosView(d.Status.Consumed), Over: MacrosView(d.Status.Over),
		})
	}
	return out
}

func (a *API) remove(c *echo.Context, param string,
	del func(ctx context.Context, id, userID uuid.UUID) error, messageKey string,
) error {
	id, err := pathID(c, param)
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err := del(ctx, id, auth.MustUser(ctx).ID); err != nil {
		return httpx.Fail(err, i18n.T(ctx, messageKey))
	}
	return c.NoContent(http.StatusNoContent)
}

func projectIngredient(in Ingredient, userID uuid.UUID) IngredientView {
	return IngredientView{
		ID: in.ID, Name: in.Name, Brand: in.Brand, Category: in.Category,
		Own: in.UserID != nil && *in.UserID == userID, ServingSizeGrams: in.ServingSizeGrams, Per100g: MacrosView(in.Per100g),
	}
}

func projectMealIngredient(mi MealIngredient) MealIngredientView {
	return MealIngredientView{
		ID: mi.ID, IngredientID: mi.IngredientID, Name: mi.IngredientName,
		QuantityGrams: mi.QuantityGrams, Macros: MacrosView(mi.Macros),
		SourceText: mi.SourceText, Estimated: mi.Estimated, Optional: mi.Optional,
	}
}

func projectMealIngredients(in []MealIngredient) []MealIngredientView {
	out := make([]MealIngredientView, 0, len(in))
	for _, mi := range in {
		out = append(out, projectMealIngredient(mi))
	}
	return out
}

func summarizePlan(p MealPlan) PlanSummary {
	return PlanSummary{
		ID: p.ID, Name: p.Name, Description: p.Description,
		PlanType: p.Settings.Type, CustomCarbPct: p.Settings.CustomCarbPct, Mode: p.Settings.Mode,
		DayCount: len(p.Days), TotalMacros: MacrosView(p.TotalMacros),
	}
}

// projectPlan is a plan with each day measured against target, when there is
// one.
func projectPlan(plan MealPlan, target *Macros) PlanDetail {
	out := PlanDetail{
		PlanSummary: summarizePlan(plan), Objective: plan.Objective, ActivityLevel: plan.ActivityLevel,
		Gender: plan.Gender, Notes: plan.Notes, Target: macrosViewPtr(target), Days: make([]PlanDayView, 0, len(plan.Days)),
	}
	var statuses []meal.DayStatus
	if target != nil {
		statuses = plan.State().Statuses(*target)
	}
	for i, d := range plan.Days {
		day := PlanDayView{
			ID: d.ID, Weekday: int(d.Weekday),
			CarbType: d.Override.CarbType, CarbG: d.Override.CarbG, ProteinG: d.Override.ProteinG, FatG: d.Override.FatG,
			Meals: make([]MealView, 0, len(d.Meals)),
		}
		if statuses != nil {
			st := statuses[i]
			day.Status = &DayStatusView{
				Target: MacrosView(st.Target), Consumed: MacrosView(st.Consumed),
				Remaining: MacrosView(st.Remaining), Over: MacrosView(st.Over), IsOver: st.IsOver(),
			}
		}
		for _, m := range d.Meals {
			mv := MealView{
				ID: m.ID, MealNumber: m.MealNumber, Name: m.Name, OptionLabel: m.OptionLabel, Optional: m.Optional,
				TotalMacros: MacrosView(m.TotalMacros), Ingredients: projectMealIngredients(m.Ingredients),
			}
			for _, alt := range m.Alternatives {
				mv.Alternatives = append(mv.Alternatives, MealOptionView{
					ID: alt.ID, OptionLabel: alt.OptionLabel, TotalMacros: MacrosView(alt.TotalMacros),
					Ingredients: projectMealIngredients(alt.Ingredients),
				})
			}
			day.Meals = append(day.Meals, mv)
		}
		out.Days = append(out.Days, day)
	}
	return out
}

func macrosViewPtr(m *Macros) *MacrosView {
	if m == nil {
		return nil
	}
	v := MacrosView(*m)
	return &v
}

func pathID(c *echo.Context, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		return uuid.Nil, httpx.Fail(apperr.ErrNotFound, i18n.T(c.Request().Context(), "nutrition.err.notfound"))
	}
	return id, nil
}

func readJSON(c *echo.Context, dst any) error {
	if err := httpx.ReadJSON(c.Response(), c.Request(), dst, httpx.ReadOptions{MaxBytes: 16 << 10}); err != nil {
		ctx := c.Request().Context()
		return httpx.Fail(localize(ctx, err), i18n.T(ctx, "nutrition.api.body"))
	}
	return nil
}
