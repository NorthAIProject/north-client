package planimport

import (
	"slices"
	"strings"
	"testing"
)

func TestBuildMealGroupsOptionsInFirstSeenOrder(t *testing.T) {
	t.Parallel()

	draft, err := buildMeal(MealReading{Name: "Plano", Rows: []MealRow{
		{Meal: "Pequeno-almoço", Option: "Opção 1", Food: "pão integral", Quantity: "2", Unit: "fatias"},
		{Meal: "Pequeno-almoço", Option: "Opção 2", Food: "iogurte natural", Quantity: "1"},
		{Meal: "Almoço", Food: "arroz", Quantity: "110", Unit: "g"},
		// Not contiguous with the rest of option 1, and still option 1.
		{Meal: "Pequeno-almoço", Option: "opção 1", Food: "queijo fresco", Quantity: "30", Unit: "g"},
		{Meal: "Pequeno-almoço", Option: "Opção 3", Food: "papas de aveia"},
	}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	meals := draft.Days[0].Meals
	if len(meals) != 2 || meals[0].Name != "Pequeno-almoço" || meals[1].Name != "Almoço" {
		t.Fatalf("meals = %+v", meals)
	}
	breakfast := meals[0]
	if breakfast.OptionLabel != "Opção 1" || len(breakfast.Foods) != 2 || breakfast.Foods[1].Food != "queijo fresco" {
		t.Fatalf("option 1 = %q %+v", breakfast.OptionLabel, breakfast.Foods)
	}
	if len(breakfast.Alternatives) != 2 || breakfast.Alternatives[0].Label != "Opção 2" || breakfast.Alternatives[1].Label != "Opção 3" {
		t.Fatalf("alternatives = %+v", breakfast.Alternatives)
	}
	if f := breakfast.Alternatives[0].Foods; len(f) != 1 || f[0].Food != "iogurte natural" || f[0].Flags == nil || f[0].Candidates == nil {
		t.Fatalf("option 2 foods = %+v, want one normalized line", f)
	}
	if lunch := meals[1]; lunch.OptionLabel != "" || lunch.Alternatives != nil || len(lunch.Foods) != 1 {
		t.Fatalf("lunch = %+v, want a plain single-option slot", lunch)
	}
}

func TestBuildMealUsesTheStandInNameAndKeepsTheSourceLine(t *testing.T) {
	t.Parallel()

	draft, err := buildMeal(MealReading{Rows: []MealRow{
		{Meal: "Lanche", Food: "pão integral", FoodEN: "Wholemeal bread", Quantity: "2", Unit: "fatias", GramsEstimate: "60"},
		{Meal: "Lanche", Food: "queijo fresco", FoodEN: "Fresh cheese", Quantity: "30", Unit: "g", GramsEstimate: "35"},
		{Meal: "Lanche", Food: "fruta", FoodEN: "Apple", GramsEstimate: "150 g"},
		{Meal: "Lanche", Food: "chá", GramsEstimate: "nada"},
	}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	foods := draft.Days[0].Meals[0].Foods

	bread := foods[0]
	if bread.Food != "Wholemeal bread" || bread.SourceText != "2 fatias pão integral" {
		t.Fatalf("bread = %q from %q", bread.Food, bread.SourceText)
	}
	if bread.Grams == nil || *bread.Grams != 60 || !bread.Estimated {
		t.Fatalf("bread grams = %v estimated = %v, want the reader's 60 g estimate", bread.Grams, bread.Estimated)
	}
	if !containsFlag(bread.Flags, `Estimated weight for "2 fatias". Check it.`) {
		t.Fatalf("bread flags = %q", bread.Flags)
	}

	// 30 g is already exact: the estimate is never preferred over the file.
	cheese := foods[1]
	if cheese.Grams != nil || cheese.Estimated || cheese.SourceText != "30 g queijo fresco" {
		t.Fatalf("cheese = %+v, want the stated 30 g left to Preview", cheese)
	}

	fruit := foods[2]
	if fruit.Grams == nil || *fruit.Grams != 150 || !fruit.Estimated || !containsFlag(fruit.Flags, `Estimated weight for "fruta". Check it.`) {
		t.Fatalf("fruit = %+v", fruit)
	}

	tea := foods[3]
	if tea.Food != "chá" || tea.Grams != nil || tea.Estimated {
		t.Fatalf("tea = %+v, want no weight from an unreadable estimate", tea)
	}
}

// A range the reader filled with an estimate is flagged once, as estimated;
// "isn't a number" is only for a quantity left with no weight at all.
func TestBuildMealFlagsAnEstimatedRangeOnce(t *testing.T) {
	t.Parallel()

	draft, err := buildMeal(MealReading{Rows: []MealRow{
		{Meal: "Almoço", Food: "arroz", FoodEN: "White rice", Quantity: "150–250", Unit: "g", GramsEstimate: "200"},
		{Meal: "Almoço", Food: "feijão", FoodEN: "Beans", Quantity: "150–250", Unit: "g"},
	}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	foods := draft.Days[0].Meals[0].Foods

	rice := foods[0]
	if rice.Grams == nil || *rice.Grams != 200 || !rice.Estimated {
		t.Fatalf("rice = %+v, want the 200 g estimate", rice)
	}
	if want := []string{`Estimated weight for "150–250 g". Check it.`}; !slices.Equal(rice.Flags, want) {
		t.Fatalf("rice flags = %q, want only %q", rice.Flags, want)
	}

	beans := foods[1]
	if beans.Grams != nil || !containsFlag(beans.Flags, `Quantity "150–250" isn't a number.`) {
		t.Fatalf("beans = %+v, want the quantity flagged when nothing filled it", beans)
	}
}

func TestBuildMealCopiesASameAsSlotOnEveryDayItAppears(t *testing.T) {
	t.Parallel()

	draft, err := buildMeal(MealReading{
		Rows: []MealRow{
			{Day: "Monday", Meal: "Almoço", Option: "Prato – carne", Food: "frango", Quantity: "125", Unit: "g"},
			{Day: "Monday", Meal: "Almoço", Option: "Prato – peixe", Food: "pescada", Quantity: "150", Unit: "g"},
			{Day: "Monday", Meal: "Lanche", Food: "maçã", Quantity: "1"},
			{Day: "Tuesday", Meal: "Almoço", Option: "A", Food: "massa", Quantity: "110", Unit: "g"},
			{Day: "Tuesday", Meal: "Almoço", Option: "B", Food: "arroz", Quantity: "110", Unit: "g"},
			{Day: "Tuesday", Meal: "JANTAR", Food: "sopa", Quantity: "300", Unit: "ml"},
			{Day: "Wednesday", Meal: "Lanche", Food: "pera", Quantity: "1"},
		},
		SameAs: []SameMeal{{Meal: "Jantar", SameAs: "almoço"}},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if draft.EveryDay {
		t.Fatalf("a plan with weekdays is not an every-day plan")
	}

	mon := draft.Days[0].Meals
	if len(mon) != 3 || mon[2].Name != "Jantar" {
		t.Fatalf("Monday meals = %+v, want Jantar added after the others", mon)
	}
	dinner := mon[2]
	if dinner.OptionLabel != "Prato – carne" || len(dinner.Alternatives) != 1 || dinner.Alternatives[0].Foods[0].Food != "pescada" {
		t.Fatalf("Monday dinner = %+v, want a copy of lunch's options", dinner)
	}
	// A copy, not the same lines: changing dinner leaves lunch alone.
	*dinner.Foods[0].Quantity = 999
	dinner.Alternatives[0].Foods[0].Food = "atum"
	if lunch := mon[0]; *lunch.Foods[0].Quantity != 125 || lunch.Alternatives[0].Foods[0].Food != "pescada" {
		t.Fatalf("lunch changed with dinner: %+v", lunch)
	}

	// An existing slot keeps its own name and takes lunch's options, with
	// its own soup kept in each of them, after lunch's foods.
	tue := draft.Days[1].Meals
	if len(tue) != 2 || tue[1].Name != "JANTAR" || len(tue[1].Alternatives) != 1 {
		t.Fatalf("Tuesday meals = %+v", tue)
	}
	tueDinner := tue[1]
	for _, opt := range tueDinner.Options() {
		if len(opt.Foods) != 2 || opt.Foods[1].Food != "sopa" {
			t.Fatalf("Tuesday dinner option %q = %+v, want lunch's food then the soup", opt.Label, opt.Foods)
		}
	}
	if tueDinner.OptionLabel != "A" || tueDinner.Foods[0].Food != "massa" || tueDinner.Alternatives[0].Foods[0].Food != "arroz" {
		t.Fatalf("Tuesday dinner = %+v, want lunch's options A and B", tueDinner)
	}
	// Each option's soup is its own line.
	*tueDinner.Foods[1].Quantity = 1
	if *tueDinner.Alternatives[0].Foods[1].Quantity != 300 {
		t.Fatalf("the options share the soup line")
	}
	if len(draft.Unparsed) != 0 {
		t.Fatalf("unparsed = %q, want none", draft.Unparsed)
	}
	// No lunch on Wednesday: nothing to copy.
	if wed := draft.Days[2].Meals; len(wed) != 1 {
		t.Fatalf("Wednesday meals = %+v", wed)
	}
}

func TestBuildMealWithoutDaysIsAnEveryDayPlan(t *testing.T) {
	t.Parallel()

	draft, err := buildMeal(MealReading{
		Rows:  []MealRow{{Meal: "Almoço", Food: "arroz", Quantity: "110", Unit: "g"}, {Meal: "Jantar", Food: "sopa"}},
		Notes: "Beber 1,5 L de água por dia.",
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if !draft.EveryDay || len(draft.Days) != 1 || draft.Days[0].Label != "Every day" || len(draft.Days[0].Meals) != 2 {
		t.Fatalf("draft = %+v, want one every-day day", draft)
	}
	if draft.Notes != "Beber 1,5 L de água por dia." {
		t.Fatalf("notes = %q", draft.Notes)
	}
}

func containsFlag(flags []string, want string) bool {
	for _, f := range flags {
		if strings.Contains(f, want) {
			return true
		}
	}
	return false
}

// A same_as whose source is nowhere in the plan is reported, not dropped,
// and no empty meal is made for it.
func TestBuildMealReportsASameAsWhoseSourceIsMissing(t *testing.T) {
	t.Parallel()

	draft, err := buildMeal(MealReading{
		Rows:   []MealRow{{Meal: "Lanche", Food: "maçã", Quantity: "1"}},
		SameAs: []SameMeal{{Meal: "Jantar", SameAs: "Almoço"}},
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if meals := draft.Days[0].Meals; len(meals) != 1 || meals[0].Name != "Lanche" {
		t.Fatalf("meals = %+v, want Lanche alone", meals)
	}
	want := "Jantar: same as Almoço — no meal called Almoço was found"
	if len(draft.Unparsed) != 1 || draft.Unparsed[0] != want {
		t.Fatalf("unparsed = %q, want %q", draft.Unparsed, want)
	}
}
