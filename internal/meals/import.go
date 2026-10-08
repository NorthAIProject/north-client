package meals

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/meals/meal"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// ImportedMealPlan is a meal plan read from a file, resolved to ingredients,
// and confirmed by the person. Everything a hand-made plan stores, nothing
// more: the plan, its meals by weekday, and each portion as an ingredient at a
// gram weight.
type ImportedMealPlan struct {
	Name          string
	PlanType      string
	CustomCarbPct *float64
	Meals         []ImportedMeal
}

// ImportedMeal is one meal on one weekday, 0–6 Sunday–Saturday.
type ImportedMeal struct {
	Name    string
	Weekday int
	Items   []ImportedItem
}

// ImportedItem is one portion. Exactly one of IngredientID and NewIngredient is
// set: an existing ingredient the person can see, or a personal one to create
// from the macros their file stated.
type ImportedItem struct {
	IngredientID  uuid.UUID
	NewIngredient *IngredientInput
	QuantityGrams float64
}

// DayCheck is one weekday measured against the person's active target.
type DayCheck struct {
	Totals  Macros
	Target  meal.DayTarget
	Overage meal.Overage
}

// ImportOverageError refuses an import with a day over its target that the
// person has not confirmed. Days lists each one, so the review screen can show
// exactly which and by how much.
type ImportOverageError struct {
	Days map[int]meal.Overage
}

func (e ImportOverageError) Error() string {
	days := make([]int, 0, len(e.Days))
	for d := range e.Days {
		days = append(days, d)
	}
	sort.Ints(days)

	parts := make([]string, 0, len(days))
	for _, d := range days {
		parts = append(parts, fmt.Sprintf("%s: %s", time.Weekday(d), OverageError{Overage: e.Days[d]}.Error()))
	}
	return strings.Join(parts, "; ")
}

// CheckDays measures each weekday's totals against the active macro target.
//
// It is the rule AddIngredientChecked applies one ingredient at a time, applied
// to a whole plan at once, and with the same exemptions: no plan type, or no
// active target, means there is nothing to measure against, and ok is false.
// The target itself is only read. An import never recalculates or replaces it.
func (s *MealPlanService) CheckDays(ctx context.Context, userID uuid.UUID, planType string, customCarbPct *float64, totals map[int]Macros, goals MacroGoalLookup) (map[int]DayCheck, bool) {
	if planType == "" || goals == nil {
		return nil, false
	}
	goal, err := goals.Current(ctx, userID)
	if err != nil {
		return nil, false
	}

	target := meal.ResolveDayTarget(goal.ProteinG, goal.FatG, goal.CarbG, planType, customCarbPct, "", nil, nil, nil)
	out := make(map[int]DayCheck, len(totals))
	for day, sum := range totals {
		out[day] = DayCheck{Totals: sum, Target: target, Overage: meal.CheckOverage(sum, target)}
	}
	return out, true
}

// ImportPlan creates a whole meal plan from an import in one transaction.
//
// Everything is validated and every macro computed before anything is
// written, and the overage rule is applied to the plan as a whole: a day over
// its target without confirmOverage refuses the import and writes nothing,
// exactly as AddIngredientChecked refuses a single portion. With confirmation,
// the meals on those days are marked overage_confirmed, as a hand-confirmed
// portion would mark its meal.
func (s *MealPlanService) ImportPlan(ctx context.Context, userID uuid.UUID, in ImportedMealPlan, confirmOverage bool, goals MacroGoalLookup) (MealPlan, error) {
	planInput, err := ValidateMealPlan(MealPlanInput{Name: in.Name, PlanType: in.PlanType, CustomCarbPct: in.CustomCarbPct})
	if err != nil {
		return MealPlan{}, err
	}
	if len(in.Meals) == 0 {
		return MealPlan{}, apperr.FieldErrors{}.Add("meals", "The plan has no meals.").OrNil()
	}

	rows := make([]importMealRow, 0, len(in.Meals))
	totals := map[int]Macros{}
	numbers := map[int]int{}

	for i, m := range in.Meals {
		numbers[m.Weekday]++
		mealIn, err := ValidateMeal(MealInput{Name: m.Name, MealNumber: numbers[m.Weekday], Weekday: &m.Weekday})
		if err != nil {
			return MealPlan{}, apperr.Wrap(err, "meal %d", i+1)
		}
		if len(m.Items) == 0 {
			return MealPlan{}, apperr.FieldErrors{}.Add("meals", fmt.Sprintf("%q on %s has no foods.", mealIn.Name, time.Weekday(m.Weekday))).OrNil()
		}

		row := importMealRow{input: mealIn}
		for _, item := range m.Items {
			resolved, err := s.resolveImportedItem(ctx, userID, item)
			if err != nil {
				return MealPlan{}, apperr.Wrap(err, "%q on %s", mealIn.Name, time.Weekday(m.Weekday))
			}
			row.items = append(row.items, resolved)
			row.total = row.total.Add(resolved.macros)
		}
		totals[m.Weekday] = totals[m.Weekday].Add(row.total)
		rows = append(rows, row)
	}

	var macroPlanID *uuid.UUID
	if goals != nil {
		if cur, err := goals.Current(ctx, userID); err == nil && cur.ID != uuid.Nil {
			macroPlanID = &cur.ID
		}
	}

	checks, _ := s.CheckDays(ctx, userID, planInput.PlanType, planInput.CustomCarbPct, totals, goals)
	over := map[int]meal.Overage{}
	for day, c := range checks {
		if c.Overage.IsOver {
			over[day] = c.Overage
		}
	}
	if len(over) > 0 && !confirmOverage {
		return MealPlan{}, ImportOverageError{Days: over}
	}
	for i := range rows {
		_, rows[i].overConfirmed = over[*rows[i].input.Weekday]
	}

	planInput.MacroPlanID = macroPlanID
	return s.repo.CreateImportedPlan(ctx, userID, planInput, rows)
}

// importMealRow is one validated meal ready to insert.
type importMealRow struct {
	input         MealInput
	items         []importItemRow
	total         Macros
	overConfirmed bool
}

type importItemRow struct {
	ingredientID  uuid.UUID
	newIngredient *Ingredient
	grams         float64
	macros        Macros
}

func (s *MealPlanService) resolveImportedItem(ctx context.Context, userID uuid.UUID, item ImportedItem) (importItemRow, error) {
	if item.QuantityGrams <= 0 {
		return importItemRow{}, apperr.FieldErrors{}.Add("quantity_grams", "Every food needs a weight in grams.").OrNil()
	}

	switch {
	case item.NewIngredient != nil && item.IngredientID == uuid.Nil:
		clean, err := ValidateIngredient(*item.NewIngredient)
		if err != nil {
			return importItemRow{}, err
		}
		ing := toIngredient(clean)
		return importItemRow{newIngredient: &ing, grams: item.QuantityGrams, macros: ing.MacrosFor(item.QuantityGrams)}, nil

	case item.NewIngredient == nil && item.IngredientID != uuid.Nil:
		ing, err := s.repo.GetIngredient(ctx, item.IngredientID, userID)
		if err != nil {
			return importItemRow{}, err
		}
		return importItemRow{ingredientID: ing.ID, grams: item.QuantityGrams, macros: ing.MacrosFor(item.QuantityGrams)}, nil

	default:
		return importItemRow{}, apperr.FieldErrors{}.Add("ingredient_id", "Choose an ingredient for every food.").OrNil()
	}
}
