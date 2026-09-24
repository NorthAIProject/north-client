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

	var plan struct {
		ID    string
		Meals []struct{ ID string }
	}
	decode(http.StatusCreated, http.MethodPost, "/api/v1/nutrition/plans", `{"name":"Training days"}`, &plan)
	decode(http.StatusCreated, http.MethodPost, "/api/v1/nutrition/plans/"+plan.ID+"/meals", `{"name":"Breakfast","mealNumber":1}`, &plan)
	if len(plan.Meals) != 1 {
		t.Fatalf("plan after adding a meal: %+v", plan)
	}
	decode(http.StatusCreated, http.MethodPost, "/api/v1/nutrition/meals/"+plan.Meals[0].ID+"/ingredients",
		`{"ingredientId":"`+oats.ID+`","quantityGrams":50}`, nil)

	var log struct {
		Entries []struct{ ID string }
		Totals  struct{ Calories float64 }
	}
	decode(http.StatusCreated, http.MethodPost, "/api/v1/nutrition/log/ingredients", `{"ingredientId":"`+oats.ID+`","quantityGrams":100}`, &log)
	decode(http.StatusCreated, http.MethodPost, "/api/v1/nutrition/log/meals", `{"mealId":"`+plan.Meals[0].ID+`"}`, &log)
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
