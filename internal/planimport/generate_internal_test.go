package planimport

import (
	"strings"
	"testing"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
)

func TestExcludedByMatchesFoodsAndTheirGroups(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		food       string
		exclusions []string
		want       string
	}{
		{"Peanut butter", []string{"peanuts"}, "peanuts"},
		{"Greek yogurt", []string{"Dairy"}, "Dairy"},
		{"Whey protein", []string{"dairy"}, "dairy"},
		{"Butter", []string{"dairy"}, "dairy"},
		{"Almond milk", []string{"dairy"}, ""},
		{"Peanut butter", []string{"dairy"}, ""},
		{"Coconut milk", []string{"lactose"}, ""},
		{"Whole wheat bread", []string{"gluten"}, "gluten"},
		{"Buckwheat", []string{"gluten"}, ""},
		{"Rice noodles", []string{"gluten"}, ""},
		{"Egg noodles", []string{"gluten"}, "gluten"},
		{"Eggplant", []string{"eggs"}, ""},
		{"Boiled eggs", []string{"eggs"}, "eggs"},
		{"Coconut", []string{"nuts"}, ""},
		{"Cashews", []string{"tree nuts"}, "tree nuts"},
		{"Smoked salmon", []string{"seafood"}, "seafood"},
		{"Shrimp", []string{"seafood"}, "seafood"},
		{"Chicken breast", []string{"red meat"}, ""},
		{"Bacon", []string{"pork"}, "pork"},
		{"Sweet potato", []string{"sweet potato"}, "sweet potato"},
		{"Potato", []string{"sweet potato"}, ""},
		{"Mushrooms", []string{"mushroom"}, "mushroom"},
		{"Oats", []string{"cogumelos"}, ""},
	} {
		got, ok := excludedBy(tc.food, tc.exclusions)
		if (tc.want != "") != ok || got != tc.want {
			t.Errorf("excludedBy(%q, %q) = %q, %v; want %q", tc.food, tc.exclusions, got, ok, tc.want)
		}
	}
}

func food(name string, grams float64) FoodDraft {
	return FoodDraft{Food: name, Grams: util.Ptr(grams), Unit: "g"}
}

func TestCheckGeneratedHoldsTheShapeTheyAskedFor(t *testing.T) {
	t.Parallel()

	in := meals.MealPlanIntake{Days: 2, MealsPerDay: 2, Exclusions: []string{"dairy"}}
	day := func(meals ...MealDraftMeal) MealDayDraft { return MealDayDraft{Label: "Day", Meals: meals} }
	good := MealDraftMeal{Name: "Lunch", Foods: []FoodDraft{food("Chicken", 150)}}

	if got := checkGenerated(MealDraft{Name: "Plan", Days: []MealDayDraft{day(good, good), day(good, good)}}, in); len(got) != 0 {
		t.Fatalf("a plan that fits was refused: %q", got)
	}

	optional := good
	optional.Optional = true
	withYogurt := MealDraftMeal{Name: "Snack", Foods: []FoodDraft{food("Greek yogurt", 170)},
		Alternatives: []MealDraftOption{{Label: "Swap", Foods: []FoodDraft{{Food: "Cheddar"}}}}}

	got := strings.Join(checkGenerated(MealDraft{Days: []MealDayDraft{day(good, optional), day(good, withYogurt)}}, in), "\n")
	for _, want := range []string{
		"the plan has no name",
		"has 1 meals but they asked for exactly 2", // the optional meal does not count
		`"Greek yogurt" in Day, Snack is dairy`,
		`"Cheddar" in Day, Snack is dairy`, // alternatives are checked too
		`"Cheddar" in Day, Snack has no weight`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("problems lack %q:\n%s", want, got)
		}
	}

	if got := checkGenerated(MealDraft{Name: "Plan", Days: []MealDayDraft{day(good, good)}}, in); len(got) != 1 || !strings.Contains(got[0], "1 days but they asked for exactly 2") {
		t.Fatalf("a day short: %q", got)
	}
}

func TestCheckFitNamesUnmatchedFoodsAndEveryMacroOffTarget(t *testing.T) {
	t.Parallel()

	matched := food("Chicken", 150)
	matched.IngredientID = util.Ptr(uuid.New())
	target := &MacroGrams{ProteinG: 150, CarbG: 70, FatG: 60}

	fits := MealDraft{Days: []MealDayDraft{{Label: "Monday", Meals: []MealDraftMeal{{Name: "Lunch", Foods: []FoodDraft{matched}}},
		Totals: &MacroGrams{ProteinG: 140, CarbG: 66, FatG: 55.4}, Target: target}}}
	if got := checkFit(fits); len(got) != 0 {
		t.Fatalf("a day at 93%% of each macro was refused: %q", got)
	}

	off := MealDraft{Days: []MealDayDraft{{Label: "Monday", Meals: []MealDraftMeal{{Name: "Lunch", Foods: []FoodDraft{matched, food("Moon cheese", 30)}}},
		Totals: &MacroGrams{ProteinG: 100, CarbG: 80, FatG: 60.4}, Target: target}}}
	got := strings.Join(checkFit(off), "\n")
	for _, want := range []string{
		`"Moon cheese" in Monday, Lunch is not a catalog food`,
		"Monday has 100 g of protein, well under its target of 150 g",
		"Monday has 80 g of carbs, over its target of 70 g by 10 g",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("problems lack %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "fat") {
		t.Errorf("fat 0.4 g over is within the overage rule's tolerance:\n%s", got)
	}
}

func TestDraftFromGeneratedIsAnEasyPlanOrOneEveryDay(t *testing.T) {
	t.Parallel()

	g := generatedPlan{Name: " High protein ", Rationale: "Protein first.", Days: []generatedDay{{
		Meals: []generatedMeal{{Name: "Breakfast", Foods: []generatedFood{{Food: "Oats", Grams: 60}, {Food: "Mystery", Grams: 0}}}},
	}}}

	d := draftFromGenerated(g, meals.MealPlanIntake{Days: 1, PlanType: meal.LowCarb})
	if d.Name != "High protein" || d.Notes != "Protein first." || d.Mode != string(meal.Easy) || d.PlanType != string(meal.LowCarb) || !d.EveryDay {
		t.Fatalf("draft = %+v", d)
	}
	foods := d.Days[0].Meals[0].Foods
	if foods[0].Grams == nil || *foods[0].Grams != 60 || foods[1].Grams != nil {
		t.Fatalf("foods = %+v; zero grams must stay unknown, not become 0", foods)
	}

	if d := draftFromGenerated(g, meals.MealPlanIntake{Days: 3}); d.EveryDay {
		t.Fatal("a reply one day short of three is not an every-day plan; the check must see it as a day short")
	}
}
