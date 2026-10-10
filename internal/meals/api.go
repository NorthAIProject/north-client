package meals

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
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

// NewAPI builds the routes from the web handler's options; mount them behind
// auth.RequireBearer.
func NewAPI(opts HandlerOptions) *API {
	return &API{
		ingredients: opts.Ingredients, plans: opts.Plans, foodLog: opts.FoodLog,
		progress: opts.Progress, recommend: opts.Recommend,
	}
}

func (a *API) Routes(r chi.Router) {
	r.Get("/nutrition/ingredients", a.searchIngredients)
	r.Post("/nutrition/ingredients", a.createIngredient)
	r.Delete("/nutrition/ingredients/{ingredientID}", a.deleteIngredient)

	r.Get("/nutrition/plan-options", a.planOptions)
	r.Get("/nutrition/plans", a.listPlans)
	r.Post("/nutrition/plans", a.createPlan)
	r.Get("/nutrition/plans/{planID}", a.showPlan)
	r.Put("/nutrition/plans/{planID}", a.updatePlan)
	r.Delete("/nutrition/plans/{planID}", a.deletePlan)
	r.Post("/nutrition/plans/{planID}/days", a.addDay)
	r.Put("/nutrition/plan-days/{dayID}", a.updateDay)
	r.Delete("/nutrition/plan-days/{dayID}", a.removeDay)
	r.Post("/nutrition/plan-days/{dayID}/meals", a.addMeal)
	r.Delete("/nutrition/meals/{mealID}", a.removeMeal)
	r.Post("/nutrition/meals/{mealID}/options", a.addOption)
	r.Post("/nutrition/meals/{mealID}/ingredients", a.addMealIngredient)
	r.Post("/nutrition/meals/{mealID}/ingredients/batch", a.addMealIngredients)
	r.Delete("/nutrition/meal-ingredients/{mealIngredientID}", a.removeMealIngredient)

	r.Get("/nutrition/log", a.showLog)
	r.Post("/nutrition/log/ingredients", a.logIngredient)
	r.Post("/nutrition/log/meals", a.logMeal)
	r.Delete("/nutrition/log/{entryID}", a.deleteLogEntry)
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
}

// MealView is a meal slot's default option, with the slot's other options.
// Only the default counts toward the day's status.
type MealView struct {
	ID           uuid.UUID            `json:"id"`
	MealNumber   int                  `json:"mealNumber"`
	Name         string               `json:"name"`
	OptionLabel  string               `json:"optionLabel,omitempty"`
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
	ConfirmOverage bool      `json:"confirmOverage"`
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

func (a *API) searchIngredients(w http.ResponseWriter, r *http.Request) {
	userID := auth.MustUser(r.Context()).ID
	list, err := a.ingredients.Search(r.Context(), userID, r.URL.Query().Get("q"), 60)
	if err != nil {
		httpx.Error(w, err, "Ingredients could not be loaded.")
		return
	}
	out := IngredientList{Ingredients: make([]IngredientView, 0, len(list))}
	for _, in := range list {
		out.Ingredients = append(out.Ingredients, projectIngredient(in, userID))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) createIngredient(w http.ResponseWriter, r *http.Request) {
	var req IngredientRequest
	if !readJSON(w, r, &req) {
		return
	}
	userID := auth.MustUser(r.Context()).ID
	in, err := a.ingredients.Create(r.Context(), userID, IngredientInput{
		Name: req.Name, Brand: req.Brand, Category: req.Category, ServingSizeGrams: req.ServingSizeGrams,
		Per100g: Macros(req.Per100g),
	})
	if err != nil {
		httpx.Error(w, err, "The ingredient could not be saved.")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, projectIngredient(in, userID))
}

func (a *API) deleteIngredient(w http.ResponseWriter, r *http.Request) {
	a.remove(w, r, "ingredientID", a.ingredients.Delete, "The ingredient could not be deleted.")
}

func (a *API) planOptions(w http.ResponseWriter, r *http.Request) {
	target, err := a.plans.ActiveTarget(r.Context(), auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "Meal plan options could not be loaded.")
		return
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
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) listPlans(w http.ResponseWriter, r *http.Request) {
	plans, err := a.plans.ListPlans(r.Context(), auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "Meal plans could not be loaded.")
		return
	}
	out := PlanList{Plans: make([]PlanSummary, 0, len(plans))}
	for _, p := range plans {
		out.Plans = append(out.Plans, summarizePlan(p))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) createPlan(w http.ResponseWriter, r *http.Request) {
	var req PlanRequest
	if !readJSON(w, r, &req) {
		return
	}
	weekdays := make([]time.Weekday, len(req.Weekdays))
	for i, wd := range req.Weekdays {
		weekdays[i] = time.Weekday(wd)
	}
	plan, err := a.plans.CreatePlan(r.Context(), auth.MustUser(r.Context()).ID, MealPlanInput{
		Name: req.Name, Description: req.Description, Objective: req.Objective,
		ActivityLevel: req.ActivityLevel, Gender: req.Gender,
		Settings: meal.PlanSettings{Type: req.PlanType, CustomCarbPct: req.CustomCarbPct, Mode: req.Mode},
		DayCount: req.DayCount, Weekdays: weekdays,
	}, nil, false)
	if err != nil {
		httpx.Error(w, err, "The meal plan could not be saved.")
		return
	}
	a.writePlan(w, r, http.StatusCreated, plan.ID)
}

func (a *API) showPlan(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "planID")
	if !ok {
		return
	}
	a.writePlan(w, r, http.StatusOK, id)
}

func (a *API) updatePlan(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "planID")
	if !ok {
		return
	}
	var req PlanSettingsRequest
	if !readJSON(w, r, &req) {
		return
	}
	err := a.plans.UpdateSettings(r.Context(), id, auth.MustUser(r.Context()).ID, PlanSettingsInput{
		Name: req.Name, Description: req.Description,
		Settings:     meal.PlanSettings{Type: req.PlanType, CustomCarbPct: req.CustomCarbPct, Mode: req.Mode},
		ConfirmReset: req.ConfirmReset, ConfirmOverage: req.ConfirmOverage,
	})
	if err != nil {
		writeChangeError(w, err, "The meal plan could not be saved.")
		return
	}
	a.writePlan(w, r, http.StatusOK, id)
}

func (a *API) deletePlan(w http.ResponseWriter, r *http.Request) {
	a.remove(w, r, "planID", a.plans.DeletePlan, "The meal plan could not be deleted.")
}

func (a *API) addDay(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "planID")
	if !ok {
		return
	}
	var req DayRequest
	if !readJSON(w, r, &req) {
		return
	}
	var weekday *time.Weekday
	if req.Weekday != nil {
		wd := time.Weekday(*req.Weekday)
		weekday = &wd
	}
	if _, err := a.plans.AddDay(r.Context(), id, auth.MustUser(r.Context()).ID, weekday); err != nil {
		httpx.Error(w, err, "The day could not be added.")
		return
	}
	a.writePlan(w, r, http.StatusCreated, id)
}

func (a *API) updateDay(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "dayID")
	if !ok {
		return
	}
	var req DayOverrideRequest
	if !readJSON(w, r, &req) {
		return
	}
	userID := auth.MustUser(r.Context()).ID
	planID, err := a.plans.PlanIDOfDay(r.Context(), id, userID)
	if err != nil {
		httpx.Error(w, err, "The day could not be saved.")
		return
	}
	err = a.plans.UpdateDay(r.Context(), id, userID, meal.DayOverride{
		CarbType: req.CarbType, CarbG: req.CarbG, ProteinG: req.ProteinG, FatG: req.FatG,
	}, req.ConfirmOverage)
	if err != nil {
		writeChangeError(w, err, "The day could not be saved.")
		return
	}
	a.writePlan(w, r, http.StatusOK, planID)
}

func (a *API) removeDay(w http.ResponseWriter, r *http.Request) {
	a.remove(w, r, "dayID", a.plans.RemoveDay, "The day could not be removed.")
}

func (a *API) addMeal(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "dayID")
	if !ok {
		return
	}
	var req MealRequest
	if !readJSON(w, r, &req) {
		return
	}
	added, err := a.plans.AddMeal(r.Context(), id, auth.MustUser(r.Context()).ID, req.Name)
	if err != nil {
		httpx.Error(w, err, "The meal could not be added.")
		return
	}
	a.writePlan(w, r, http.StatusCreated, added.MealPlanID)
}

// addOption adds an empty option to the meal mealID — any of its options —
// and answers with the whole plan, as adding a meal does, so a client
// refreshes in one call.
func (a *API) addOption(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "mealID")
	if !ok {
		return
	}
	var req OptionRequest
	if !readJSON(w, r, &req) {
		return
	}
	userID := auth.MustUser(r.Context()).ID
	planID, err := a.plans.PlanIDOfMeal(r.Context(), id, userID)
	if err != nil {
		httpx.Error(w, err, "The option could not be added.")
		return
	}
	if _, err = a.plans.AddOption(r.Context(), userID, planID, id, req.Label); err != nil {
		httpx.Error(w, err, "The option could not be added.")
		return
	}
	a.writePlan(w, r, http.StatusCreated, planID)
}

func (a *API) removeMeal(w http.ResponseWriter, r *http.Request) {
	a.remove(w, r, "mealID", a.plans.RemoveMeal, "The meal could not be removed.")
}

func (a *API) addMealIngredient(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "mealID")
	if !ok {
		return
	}
	var req PortionRequest
	if !readJSON(w, r, &req) {
		return
	}
	added, err := a.plans.AddIngredient(r.Context(), id, auth.MustUser(r.Context()).ID,
		MealIngredientInput{IngredientID: req.IngredientID, QuantityGrams: req.QuantityGrams}, req.ConfirmOverage)
	if err != nil {
		writeChangeError(w, err, "The ingredient could not be added.")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, projectMealIngredient(added))
}

func (a *API) addMealIngredients(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "mealID")
	if !ok {
		return
	}
	var req PortionsRequest
	if !readJSON(w, r, &req) {
		return
	}
	lines := make([]MealIngredientInput, len(req.Portions))
	for i, p := range req.Portions {
		lines[i] = MealIngredientInput{IngredientID: p.IngredientID, QuantityGrams: p.QuantityGrams}
	}
	added, err := a.plans.AddIngredients(r.Context(), id, auth.MustUser(r.Context()).ID, lines, req.ConfirmOverage)
	if err != nil {
		writeChangeError(w, err, "The ingredients could not be added.")
		return
	}
	out := MealPortionList{Portions: make([]MealIngredientView, 0, len(added))}
	for _, mi := range added {
		out.Portions = append(out.Portions, projectMealIngredient(mi))
	}
	httpx.WriteJSON(w, http.StatusCreated, out)
}

func (a *API) removeMealIngredient(w http.ResponseWriter, r *http.Request) {
	a.remove(w, r, "mealIngredientID", a.plans.RemoveIngredient, "The ingredient could not be removed.")
}

func (a *API) showLog(w http.ResponseWriter, r *http.Request) {
	a.writeLog(w, r, http.StatusOK)
}

func (a *API) logIngredient(w http.ResponseWriter, r *http.Request) {
	var req PortionRequest
	if !readJSON(w, r, &req) {
		return
	}
	user := auth.MustUser(r.Context())
	if _, err := a.foodLog.LogIngredient(r.Context(), user.ID, LogIngredientInput{
		IngredientID: req.IngredientID, QuantityGrams: req.QuantityGrams, LogDate: time.Now().In(user.Location()),
	}); err != nil {
		httpx.Error(w, err, "That could not be logged.")
		return
	}
	a.writeLog(w, r, http.StatusCreated)
}

func (a *API) logMeal(w http.ResponseWriter, r *http.Request) {
	var req LogMealRequest
	if !readJSON(w, r, &req) {
		return
	}
	user := auth.MustUser(r.Context())
	if _, err := a.foodLog.LogMeal(r.Context(), user.ID, LogMealInput{MealID: req.MealID, LogDate: time.Now().In(user.Location())}); err != nil {
		httpx.Error(w, err, "The meal could not be logged.")
		return
	}
	a.writeLog(w, r, http.StatusCreated)
}

func (a *API) deleteLogEntry(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "entryID")
	if !ok {
		return
	}
	if err := a.foodLog.Delete(r.Context(), id, auth.MustUser(r.Context()).ID); err != nil {
		httpx.Error(w, err, "The entry could not be deleted.")
		return
	}
	a.writeLog(w, r, http.StatusOK)
}

// writeLog answers with today's log as the web page shows it: the entries,
// the totals, and progress against the macro goal once there is one.
func (a *API) writeLog(w http.ResponseWriter, r *http.Request, status int) {
	user := auth.MustUser(r.Context())
	today := time.Now().In(user.Location())
	entries, err := a.foodLog.Day(r.Context(), user.ID, today)
	if err != nil {
		httpx.Error(w, err, "The food log could not be loaded.")
		return
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

	progress, err := a.progress.ForDay(r.Context(), user.ID, today)
	switch {
	case err == nil:
		p := &NutritionProgress{Goal: MacrosView(progress.Goal), Logged: MacrosView(progress.Logged), Summary: progress.Summary()}
		if rec, recErr := a.recommend.Recommend(r.Context(), user.ID); recErr == nil {
			p.Recommendation = rec.Message
		} else if !apperr.Is(recErr, apperr.ErrNotFound) {
			httpx.Error(w, recErr, "The food log could not be loaded.")
			return
		}
		out.Progress = p
	case !apperr.Is(err, apperr.ErrNotFound):
		httpx.Error(w, err, "The food log could not be loaded.")
		return
	}
	httpx.WriteJSON(w, status, out)
}

func (a *API) writePlan(w http.ResponseWriter, r *http.Request, status int, id uuid.UUID) {
	userID := auth.MustUser(r.Context()).ID
	plan, err := a.plans.GetPlan(r.Context(), id, userID)
	if err != nil {
		httpx.Error(w, err, "The meal plan could not be loaded.")
		return
	}
	target, err := a.plans.ActiveTarget(r.Context(), userID)
	if err != nil {
		httpx.Error(w, err, "The meal plan could not be loaded.")
		return
	}
	httpx.WriteJSON(w, status, projectPlan(plan, target))
}

// writeChangeError answers a refused change: a 409 MacroOverage when it would
// take a day over its target, the usual error body otherwise.
func writeChangeError(w http.ResponseWriter, err error, message string) {
	var over *OverageError
	if !errors.As(err, &over) {
		httpx.Error(w, err, message)
		return
	}
	httpx.WriteJSON(w, http.StatusConflict, ProjectOverage(over))
}

// ProjectOverage is the 409 body for a refused change. Exported so plan
// import answers an over-target plan in the same shape as the plan pages.
func ProjectOverage(over *OverageError) MacroOverage {
	out := MacroOverage{Message: over.Error(), CanConfirm: over.Verdict.CanConfirm, Days: make([]DayOverageView, 0, len(over.Verdict.Over))}
	for _, d := range over.Verdict.Over {
		out.Days = append(out.Days, DayOverageView{
			DayID: d.DayID, Weekday: int(d.Weekday),
			Target: MacrosView(d.Status.Target), Consumed: MacrosView(d.Status.Consumed), Over: MacrosView(d.Status.Over),
		})
	}
	return out
}

func (a *API) remove(w http.ResponseWriter, r *http.Request, param string,
	del func(ctx context.Context, id, userID uuid.UUID) error, message string,
) {
	id, ok := pathID(w, r, param)
	if !ok {
		return
	}
	if err := del(r.Context(), id, auth.MustUser(r.Context()).ID); err != nil {
		httpx.Error(w, err, message)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
		SourceText: mi.SourceText, Estimated: mi.Estimated,
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
				ID: m.ID, MealNumber: m.MealNumber, Name: m.Name, OptionLabel: m.OptionLabel,
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

func pathID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return uuid.Nil, false
	}
	return id, true
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := httpx.ReadJSON(w, r, dst, httpx.ReadOptions{MaxBytes: 16 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return false
	}
	return true
}
