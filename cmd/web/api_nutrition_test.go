package main

import (
	"encoding/json"
	"math"
	"net/http"
	"testing"

	"github.com/NorthAIProject/north-client/internal/config"
)

// An ingredient, a plan with a meal made of it, and a day's log of both.
func TestNutritionAPI(t *testing.T) {
	handler, pool := testRoutesAndPool(t, func(*config.Config) {})
	api := apiClient{t: t, handler: handler, bearer: "Bearer " + signIn(t, pool).Value}
	decode := func(code int, method, path, body string, into any) {
		t.Helper()
		rec := api.call(method, path, body)
		if rec.Code != code {
			t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body)
		}
		if into != nil {
			_ = json.Unmarshal(rec.Body.Bytes(), into)
		}
	}

	var oats struct{ ID string }
	decode(http.StatusCreated, http.MethodPost, "/api/v1/nutrition/ingredients",
		`{"name":"Test oats","servingSizeGrams":40,"per100g":{"calories":380,"proteinG":13,"fatG":7,"carbG":68}}`, &oats)

	var found struct {
		Ingredients []struct {
			ID  string
			Own bool
		}
	}
	decode(http.StatusOK, http.MethodGet, "/api/v1/nutrition/ingredients?q=Test+oats", "", &found)
	own := false
	for _, in := range found.Ingredients {
		own = own || (in.ID == oats.ID && in.Own)
	}
	if !own {
		t.Errorf("search did not return the new ingredient as own: %+v", found)
	}

	// Plans are held to the calculator's target, so there is none without one.
	if rec := api.call(http.MethodPost, "/api/v1/nutrition/plans", `{"name":"Too soon","planType":"mid_carb","mode":"easy","dayCount":1}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("plan without a target: %d %s", rec.Code, rec.Body)
	}
	decode(http.StatusOK, http.MethodPut, "/api/v1/calculator/biometrics", `{"weightKg":72,"heightCm":175,"dateOfBirth":"1990-05-01","sex":"female"}`, nil)
	decode(http.StatusCreated, http.MethodPost, "/api/v1/calculator/plan", `{"activityLevel":"moderate","goal":"maintenance","macroSplit":"moderate_carb"}`, nil)

	var options struct {
		Target    *struct{ CarbG float64 }
		PlanTypes []struct{ ID string }
	}
	decode(http.StatusOK, http.MethodGet, "/api/v1/nutrition/plan-options", "", &options)
	if options.Target == nil || len(options.PlanTypes) != 5 {
		t.Fatalf("plan options = %+v", options)
	}

	type day struct {
		ID      string
		Weekday int
		Status  *struct{ Target struct{ CarbG float64 } }
		Meals   []struct{ ID string }
	}
	var plan struct {
		ID   string
		Days []day
	}
	decode(http.StatusCreated, http.MethodPost, "/api/v1/nutrition/plans", `{"name":"Training days","planType":"no_carb","mode":"advanced","weekdays":[6,1]}`, &plan)
	if len(plan.Days) != 2 || plan.Days[0].Weekday != 1 || plan.Days[1].Weekday != 6 || plan.Days[0].Status == nil {
		t.Fatalf("plan = %+v, want Monday then Saturday, measured", plan)
	}
	decode(http.StatusOK, http.MethodPut, "/api/v1/nutrition/plan-days/"+plan.Days[1].ID, `{"carbType":"high_carb"}`, &plan)
	if plan.Days[1].Status.Target.CarbG <= plan.Days[0].Status.Target.CarbG {
		t.Errorf("Saturday's high-carb target is not above Monday's: %+v", plan.Days)
	}
	decode(http.StatusCreated, http.MethodPost, "/api/v1/nutrition/plan-days/"+plan.Days[0].ID+"/meals", `{"name":"Breakfast"}`, &plan)
	if len(plan.Days[0].Meals) != 1 {
		t.Fatalf("plan after adding a meal: %+v", plan)
	}
	breakfast := plan.Days[0].Meals[0].ID

	// 50 g of oats is 34 g of carbs, past a no-carb Monday: refused with the
	// amount until confirmed.
	var over struct {
		CanConfirm bool
		Days       []struct{ Over struct{ CarbG float64 } }
	}
	portion := `{"ingredientId":"` + oats.ID + `","quantityGrams":50}`
	decode(http.StatusConflict, http.MethodPost, "/api/v1/nutrition/meals/"+breakfast+"/ingredients", portion, &over)
	if !over.CanConfirm || len(over.Days) != 1 || over.Days[0].Over.CarbG <= 0 {
		t.Fatalf("overage = %+v", over)
	}
	decode(http.StatusCreated, http.MethodPost, "/api/v1/nutrition/meals/"+breakfast+"/ingredients",
		`{"ingredientId":"`+oats.ID+`","quantityGrams":50,"confirmOverage":true}`, nil)

	var log struct {
		Entries []struct{ ID string }
		Totals  struct{ Calories float64 }
	}
	decode(http.StatusCreated, http.MethodPost, "/api/v1/nutrition/log/ingredients", `{"ingredientId":"`+oats.ID+`","quantityGrams":100}`, &log)
	decode(http.StatusCreated, http.MethodPost, "/api/v1/nutrition/log/meals", `{"mealId":"`+breakfast+`"}`, &log)
	// 100 g of oats (380) plus the breakfast of 50 g (190).
	if len(log.Entries) != 2 || math.Abs(log.Totals.Calories-570) > 0.5 {
		t.Errorf("log = %+v, want two entries totalling 570 kcal", log)
	}

	decode(http.StatusOK, http.MethodDelete, "/api/v1/nutrition/log/"+log.Entries[0].ID, "", &log)
	if len(log.Entries) != 1 {
		t.Errorf("after delete, %d entries", len(log.Entries))
	}
	// Deleting what was logged keeps the log: an entry is a snapshot.
	decode(http.StatusNoContent, http.MethodDelete, "/api/v1/nutrition/plans/"+plan.ID, "", nil)
	decode(http.StatusNoContent, http.MethodDelete, "/api/v1/nutrition/ingredients/"+oats.ID, "", nil)
	decode(http.StatusOK, http.MethodGet, "/api/v1/nutrition/log", "", &log)
	if len(log.Entries) != 1 || math.Abs(log.Totals.Calories-190) > 0.5 {
		t.Errorf("log after deleting its sources = %+v, want the breakfast entry kept", log)
	}
	if missing := api.call(http.MethodGet, "/api/v1/nutrition/plans/"+plan.ID, ""); missing.Code != http.StatusNotFound {
		t.Errorf("deleted plan: %d, want 404", missing.Code)
	}
}
