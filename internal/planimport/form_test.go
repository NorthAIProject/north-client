package planimport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func optionsDraft() MealDraft {
	return MealDraft{Name: "Plano", EveryDay: true, Notes: "Beber água.", Days: []MealDayDraft{{Label: "Every day", Meals: []MealDraftMeal{{
		Name: "Almoço", OptionLabel: "Prato – carne",
		Foods: []FoodDraft{{Food: "Chicken", SourceText: "125 g frango"}},
		Alternatives: []MealDraftOption{
			{Label: "Prato – peixe", Foods: []FoodDraft{{Food: "Hake", Estimated: true}, {Food: "Rice"}}},
			{Label: "Refeição rápida 1", Foods: []FoodDraft{{Food: "Tuna"}}},
		},
	}}}}}
}

func reviewRequest(t *testing.T, d MealDraft, form url.Values) *http.Request {
	t.Helper()
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	form.Set("draft", string(raw))
	req := httptest.NewRequest(http.MethodPost, "/app/nutrition/import/review", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

// Each option's foods are posted under their own o index and read back into
// the option they came from: o0 the first, oN the Nth alternative.
func TestMealFormReadsEachOptionsFoods(t *testing.T) {
	t.Parallel()

	fish := uuid.New()
	req := reviewRequest(t, optionsDraft(), url.Values{
		"d0.m0.name":             {"Almoço"},
		"d0.m0.o0.f0.grams":      {"125"},
		"d0.m0.o1.f0.grams":      {"150"},
		"d0.m0.o1.f0.ingredient": {fish.String()},
		"d0.m0.o1.f1.ingredient": {"mine"},
		"d0.m0.o2.f0.grams":      {"80"},
		"action":                 {"update"},
	})
	d, a, err := mealFromForm(req)
	if err != nil {
		t.Fatal(err)
	}
	if a.verb != "update" || !d.EveryDay || d.Notes != "Beber água." {
		t.Fatalf("draft = %+v, action = %+v", d, a)
	}
	m := d.Days[0].Meals[0]
	if *m.Foods[0].Grams != 125 || m.Foods[0].SourceText != "125 g frango" {
		t.Fatalf("option 1 = %+v", m.Foods)
	}
	hake, rice := m.Alternatives[0].Foods[0], m.Alternatives[0].Foods[1]
	if *hake.Grams != 150 || *hake.IngredientID != fish || !hake.Estimated || !rice.SaveAsMine {
		t.Fatalf("option 2 = %+v", m.Alternatives[0].Foods)
	}
	if *m.Alternatives[1].Foods[0].Grams != 80 {
		t.Fatalf("option 3 = %+v", m.Alternatives[1].Foods)
	}
}

func TestMealFormDeletesAnOption(t *testing.T) {
	t.Parallel()

	d := optionsDraft()
	applyMealAction(&d, parseAction("delete-option:0:0:1"))
	m := d.Days[0].Meals[0]
	if len(m.Alternatives) != 1 || m.Alternatives[0].Label != "Refeição rápida 1" || m.OptionLabel != "Prato – carne" {
		t.Fatalf("meal = %+v, want the fish option gone", m)
	}

	// Removing the first option promotes the next one to be counted.
	applyMealAction(&d, parseAction("delete-option:0:0:0"))
	m = d.Days[0].Meals[0]
	if m.OptionLabel != "Refeição rápida 1" || m.Foods[0].Food != "Tuna" || len(m.Alternatives) != 0 {
		t.Fatalf("meal = %+v, want the quick meal promoted", m)
	}

	// The last option going takes the meal, and the empty day, with it.
	applyMealAction(&d, parseAction("delete-option:0:0:0"))
	if len(d.Days) != 0 {
		t.Fatalf("days = %+v, want none left", d.Days)
	}
}

func TestMealFormDeletingAnOptionsLastFoodDeletesTheOption(t *testing.T) {
	t.Parallel()

	d := optionsDraft()
	applyMealAction(&d, parseAction("delete-food:0:0:1:0"))
	if got := d.Days[0].Meals[0].Alternatives[0].Foods; len(got) != 1 || got[0].Food != "Rice" {
		t.Fatalf("option 2 = %+v, want the hake gone", got)
	}
	applyMealAction(&d, parseAction("delete-food:0:0:2:0"))
	if alts := d.Days[0].Meals[0].Alternatives; len(alts) != 1 || alts[0].Label != "Prato – peixe" {
		t.Fatalf("alternatives = %+v, want the emptied quick meal gone", alts)
	}
	applyMealAction(&d, parseAction("delete-food:0:0:0:0"))
	if m := d.Days[0].Meals[0]; m.OptionLabel != "Prato – peixe" || m.Foods[0].Food != "Rice" {
		t.Fatalf("meal = %+v, want the fish option promoted", m)
	}
}
