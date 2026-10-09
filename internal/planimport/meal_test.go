package planimport

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
)

// readyFood is a line Preview has fully resolved.
func readyFood(name string, grams float64) FoodDraft {
	id := uuid.New()
	return FoodDraft{Food: name, Grams: &grams, IngredientID: &id, Macros: &MacroGrams{ProteinG: 10}}
}

func everyDayDraft(mode meal.Mode) MealDraft {
	breakfast := readyFood("Wholemeal bread", 60)
	breakfast.SourceText, breakfast.Estimated = "2 fatias pão integral", true

	mine := FoodDraft{
		Food: "Coach's shake", Grams: util.Ptr(300.0), SaveAsMine: true, Macros: &MacroGrams{ProteinG: 72},
		StatedProteinG: util.Ptr(72.0), StatedCarbG: util.Ptr(6.0), StatedFatG: util.Ptr(6.0),
	}
	return MealDraft{
		Name: "Plano", PlanType: string(meal.LowCarb), Mode: string(mode), HasTarget: true, EveryDay: true,
		Notes: "Beber água.",
		Days: []MealDayDraft{{Label: "Every day", Meals: []MealDraftMeal{{
			Name: "Pequeno-almoço", OptionLabel: "Opção 1", Foods: []FoodDraft{breakfast},
			Alternatives: []MealDraftOption{
				{Label: "Opção 2", Foods: []FoodDraft{readyFood("Plain yogurt", 125), mine}},
				{Label: "", Foods: []FoodDraft{readyFood("Oats", 40)}},
			},
		}}}},
	}
}

func TestMealPlanFromAnEveryDayDraftSavesSevenDaysWithTheirOptions(t *testing.T) {
	t.Parallel()

	plan, problems := mealPlanFromDraft(everyDayDraft(meal.Easy))
	if len(problems) > 0 {
		t.Fatalf("problems = %q", problems)
	}
	if plan.input.DayCount != 7 || plan.input.Weekdays != nil || plan.input.Notes != "Beber água." {
		t.Fatalf("input = %+v, want seven easy days and the notes", plan.input)
	}
	// The personal food is listed once, against the single drafted day.
	if len(plan.mine) != 1 || plan.mine[0] != (minePortion{day: 0, meal: 0, option: 1, portion: 1, ingredient: plan.mine[0].ingredient}) {
		t.Fatalf("mine = %+v, want option 2's second food", plan.mine)
	}

	days := plan.daysToSave()
	if len(days) != 7 {
		t.Fatalf("days = %d, want 7", len(days))
	}
	for i, day := range days {
		slot := day.Meals[0]
		if slot.Name != "Pequeno-almoço" || slot.OptionLabel != "Opção 1" || len(slot.Alternatives) != 2 {
			t.Fatalf("day %d slot = %+v", i, slot)
		}
		if slot.Alternatives[0].Label != "Opção 2" || len(slot.Alternatives[0].Portions) != 2 {
			t.Fatalf("day %d option 2 = %+v", i, slot.Alternatives[0])
		}
		// An unlabelled option is named by its place in the slot.
		if slot.Alternatives[1].Label != "Option 3" {
			t.Fatalf("day %d option 3 label = %q", i, slot.Alternatives[1].Label)
		}
		if p := slot.Portions[0]; p.SourceText != "2 fatias pão integral" || !p.Estimated || p.QuantityGrams != 60 {
			t.Fatalf("day %d bread = %+v", i, p)
		}
	}
	// Seven copies, not seven views of one: an id filled in on one day after
	// the personal food is created must not leak into another.
	days[0].Meals[0].Alternatives[0].Portions[1].IngredientID = uuid.New()
	if days[1].Meals[0].Alternatives[0].Portions[1].IngredientID != uuid.Nil {
		t.Fatalf("the days share their portions")
	}
}

func TestMealPlanFromAnAdvancedEveryDayDraftNamesEveryWeekday(t *testing.T) {
	t.Parallel()

	plan, problems := mealPlanFromDraft(everyDayDraft(meal.Advanced))
	if len(problems) > 0 {
		t.Fatalf("problems = %q, want no weekday to choose", problems)
	}
	if plan.input.DayCount != 0 || !slices.Equal(plan.input.Weekdays, meal.WeekOrder) {
		t.Fatalf("input = %+v, want all seven weekdays", plan.input)
	}

	two := everyDayDraft(meal.Easy)
	two.Days = append(two.Days, two.Days[0])
	if _, problems := mealPlanFromDraft(two); len(problems) == 0 || !strings.Contains(strings.Join(problems, " "), "one day") {
		t.Fatalf("problems = %q, want an every-day plan held to one day", problems)
	}
}

func TestMealPlanFromDraftNeedsEveryOptionReady(t *testing.T) {
	t.Parallel()

	d := everyDayDraft(meal.Easy)
	d.Days[0].Meals[0].Alternatives[0].Foods[0].IngredientID = nil
	d.Days[0].Meals[0].Alternatives[0].Foods[0].Checks = []string{"Pick the ingredient this is."}
	_, problems := mealPlanFromDraft(d)
	if got := strings.Join(problems, " "); !strings.Contains(got, "Opção 2") || !strings.Contains(got, "Plain yogurt") {
		t.Fatalf("problems = %q, want the option and the food named", got)
	}
}

func TestMealPlanFromDraftPromotesAnOptionWhenTheFirstIsEmpty(t *testing.T) {
	t.Parallel()

	d := everyDayDraft(meal.Easy)
	d.Days[0].Meals[0].Foods = nil
	plan, problems := mealPlanFromDraft(d)
	if len(problems) > 0 {
		t.Fatalf("problems = %q", problems)
	}
	slot := plan.days[0].Meals[0]
	if slot.OptionLabel != "Opção 2" || len(slot.Portions) != 2 || len(slot.Alternatives) != 1 || slot.Alternatives[0].Label != "Option 2" {
		t.Fatalf("slot = %+v, want option 2 promoted to the default", slot)
	}
	if plan.mine[0].option != 0 || plan.mine[0].portion != 1 {
		t.Fatalf("mine = %+v, want it to follow the promoted option", plan.mine)
	}
}

// The meals service refuses an option label over its cap; a file's long one
// is cut to fit rather than failing the whole import.
func TestMealPlanFromDraftCutsALongOptionLabel(t *testing.T) {
	t.Parallel()

	d := everyDayDraft(meal.Easy)
	d.Days[0].Meals[0].Alternatives[0].Label = strings.Repeat("ç", meals.MaxOptionLabelRunes+20)
	plan, problems := mealPlanFromDraft(d)
	if len(problems) > 0 {
		t.Fatalf("problems = %q", problems)
	}
	got := plan.days[0].Meals[0].Alternatives[0].Label
	if utf8.RuneCountInString(got) != meals.MaxOptionLabelRunes || !strings.HasSuffix(got, "…") {
		t.Fatalf("label = %q (%d runes), want it cut to %d ending in …", got, utf8.RuneCountInString(got), meals.MaxOptionLabelRunes)
	}
}
