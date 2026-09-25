package meals

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
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

	r.Get("/nutrition/plans", a.listPlans)
	r.Post("/nutrition/plans", a.createPlan)
	r.Get("/nutrition/plans/{planID}", a.showPlan)
	r.Delete("/nutrition/plans/{planID}", a.deletePlan)
	r.Post("/nutrition/plans/{planID}/meals", a.addMeal)
	r.Delete("/nutrition/meals/{mealID}", a.removeMeal)
	r.Post("/nutrition/meals/{mealID}/ingredients", a.addMealIngredient)
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
}

type MealView struct {
	ID          uuid.UUID            `json:"id"`
	MealNumber  int                  `json:"mealNumber"`
	Name        string               `json:"name"`
	TotalMacros MacrosView           `json:"totalMacros"`
	Ingredients []MealIngredientView `json:"ingredients"`
}

type PlanSummary struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	TotalMacros MacrosView `json:"totalMacros"`
	MealCount   int        `json:"mealCount"`
}

type PlanList struct {
	Plans []PlanSummary `json:"plans"`
}

type PlanDetail struct {
	PlanSummary
	Objective     string     `json:"objective"`
	ActivityLevel string     `json:"activityLevel"`
	Gender        string     `json:"gender"`
	Meals         []MealView `json:"meals"`
}

type PlanRequest struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	Objective     string `json:"objective"`
	ActivityLevel string `json:"activityLevel"`
	Gender        string `json:"gender"`
}

type MealRequest struct {
	Name       string `json:"name"`
	MealNumber int    `json:"mealNumber"`
}

type PortionRequest struct {
	IngredientID  uuid.UUID `json:"ingredientId"`
	QuantityGrams float64   `json:"quantityGrams"`
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
	plan, err := a.plans.CreatePlan(r.Context(), auth.MustUser(r.Context()).ID, MealPlanInput(req))
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

func (a *API) deletePlan(w http.ResponseWriter, r *http.Request) {
	a.remove(w, r, "planID", a.plans.DeletePlan, "The meal plan could not be deleted.")
}

func (a *API) addMeal(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "planID")
	if !ok {
		return
	}
	var req MealRequest
	if !readJSON(w, r, &req) {
		return
	}
	if _, err := a.plans.AddMeal(r.Context(), id, auth.MustUser(r.Context()).ID, MealInput(req)); err != nil {
		httpx.Error(w, err, "The meal could not be added.")
		return
	}
	a.writePlan(w, r, http.StatusCreated, id)
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
	added, err := a.plans.AddIngredient(r.Context(), id, auth.MustUser(r.Context()).ID, MealIngredientInput(req))
	if err != nil {
		httpx.Error(w, err, "The ingredient could not be added.")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, projectMealIngredient(added))
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
	plan, err := a.plans.GetPlan(r.Context(), id, auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "The meal plan could not be loaded.")
		return
	}
	out := PlanDetail{
		PlanSummary: summarizePlan(plan), Objective: plan.Objective, ActivityLevel: plan.ActivityLevel,
		Gender: plan.Gender, Meals: make([]MealView, 0, len(plan.Meals)),
	}
	for _, m := range plan.Meals {
		meal := MealView{
			ID: m.ID, MealNumber: m.MealNumber, Name: m.Name, TotalMacros: MacrosView(m.TotalMacros),
			Ingredients: make([]MealIngredientView, 0, len(m.Ingredients)),
		}
		for _, mi := range m.Ingredients {
			meal.Ingredients = append(meal.Ingredients, projectMealIngredient(mi))
		}
		out.Meals = append(out.Meals, meal)
	}
	httpx.WriteJSON(w, status, out)
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
	}
}

func summarizePlan(p MealPlan) PlanSummary {
	return PlanSummary{ID: p.ID, Name: p.Name, Description: p.Description, TotalMacros: MacrosView(p.TotalMacros), MealCount: len(p.Meals)}
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
