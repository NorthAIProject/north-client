package meals

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/meals/meal"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type MealPlanService struct {
	repo *Repository
}

func NewMealPlanService(repo *Repository) *MealPlanService {
	return &MealPlanService{repo: repo}
}

type MealPlanInput struct {
	Name          string
	Description   string
	Objective     string
	ActivityLevel string
	Gender        string
	PlanType      string
	CustomCarbPct *float64
	MacroPlanID   *uuid.UUID
}

func ValidateMealPlan(in MealPlanInput) (MealPlanInput, error) {
	var errs apperr.FieldErrors

	in.Name = strings.TrimSpace(in.Name)
	switch {
	case in.Name == "":
		errs = errs.Add("name", "Give the plan a name.")
	case len(in.Name) > 200:
		errs = errs.Add("name", "Keep the name under 200 characters.")
	}

	in.PlanType = strings.TrimSpace(in.PlanType)
	if in.PlanType != "" {
		if !slices.Contains(meal.PlanTypes, in.PlanType) {
			errs = errs.Add("plan_type", "Choose a valid plan type.")
		}
		if in.PlanType == meal.PlanTypeCustom {
			if in.CustomCarbPct == nil || *in.CustomCarbPct < 0 || *in.CustomCarbPct > 100 {
				errs = errs.Add("custom_carb_pct", "Enter a percentage between 0 and 100.")
			}
		}
	}

	return in, errs.OrNil()
}

type MealInput struct {
	Name              string
	MealNumber        int
	Weekday           *int
	DayPlanType       string
	DayCustomCarbG    *float64
	DayCustomProteinG *float64
	DayCustomFatG     *float64
}

func ValidateMeal(in MealInput) (MealInput, error) {
	var errs apperr.FieldErrors

	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		errs = errs.Add("name", "Give the meal a name.")
	}
	if in.MealNumber < 1 {
		errs = errs.Add("meal_number", "Meal order must be at least 1.")
	}
	if in.Weekday != nil && (*in.Weekday < 0 || *in.Weekday > 6) {
		errs = errs.Add("weekday", "Weekday must be between 0 (Sunday) and 6 (Saturday).")
	}
	in.DayPlanType = strings.TrimSpace(in.DayPlanType)
	if in.DayPlanType != "" && !slices.Contains(meal.PlanTypes, in.DayPlanType) {
		errs = errs.Add("day_plan_type", "Choose a valid plan type.")
	}
	if in.DayCustomCarbG != nil && *in.DayCustomCarbG < 0 {
		errs = errs.Add("day_custom_carb_g", "Carb grams cannot be negative.")
	}
	if in.DayCustomProteinG != nil && *in.DayCustomProteinG < 0 {
		errs = errs.Add("day_custom_protein_g", "Protein grams cannot be negative.")
	}
	if in.DayCustomFatG != nil && *in.DayCustomFatG < 0 {
		errs = errs.Add("day_custom_fat_g", "Fat grams cannot be negative.")
	}

	return in, errs.OrNil()
}

type MealIngredientInput struct {
	IngredientID  uuid.UUID
	QuantityGrams float64
}

func ValidateMealIngredient(in MealIngredientInput) (MealIngredientInput, error) {
	var errs apperr.FieldErrors

	if in.IngredientID == uuid.Nil {
		errs = errs.Add("ingredient_id", "Choose an ingredient.")
	}
	if in.QuantityGrams <= 0 {
		errs = errs.Add("quantity_grams", "Enter a quantity greater than zero.")
	}

	return in, errs.OrNil()
}

func (s *MealPlanService) CreatePlan(ctx context.Context, userID uuid.UUID, in MealPlanInput) (MealPlan, error) {
	clean, err := ValidateMealPlan(in)
	if err != nil {
		return MealPlan{}, err
	}
	return s.repo.CreatePlan(ctx, userID, clean.Name, clean.Description, clean.Objective, clean.ActivityLevel, clean.Gender, clean.PlanType, clean.CustomCarbPct, clean.MacroPlanID)
}

func (s *MealPlanService) GetPlan(ctx context.Context, id, userID uuid.UUID) (MealPlan, error) {
	return s.repo.GetPlan(ctx, id, userID)
}

func (s *MealPlanService) ListPlans(ctx context.Context, userID uuid.UUID) ([]MealPlan, error) {
	return s.repo.ListPlans(ctx, userID)
}

func (s *MealPlanService) UpdatePlan(ctx context.Context, id, userID uuid.UUID, in MealPlanInput) (MealPlan, error) {
	clean, err := ValidateMealPlan(in)
	if err != nil {
		return MealPlan{}, err
	}
	return s.repo.UpdatePlan(ctx, id, userID, clean.Name, clean.Description, clean.Objective, clean.ActivityLevel, clean.Gender, clean.PlanType, clean.CustomCarbPct, clean.MacroPlanID)
}

func (s *MealPlanService) DeletePlan(ctx context.Context, id, userID uuid.UUID) error {
	return s.repo.DeletePlan(ctx, id, userID)
}

func (s *MealPlanService) AddMeal(ctx context.Context, planID, userID uuid.UUID, in MealInput) (Meal, error) {
	clean, err := ValidateMeal(in)
	if err != nil {
		return Meal{}, err
	}
	return s.repo.AddMeal(ctx, planID, userID, clean.Name, clean.MealNumber, clean.Weekday, clean.DayPlanType, clean.DayCustomCarbG, clean.DayCustomProteinG, clean.DayCustomFatG)
}

func (s *MealPlanService) UpdateMealDay(ctx context.Context, mealID, userID uuid.UUID, in MealInput) (Meal, error) {
	clean, err := ValidateMeal(in)
	if err != nil {
		return Meal{}, err
	}
	return s.repo.UpdateMealDay(ctx, mealID, userID, clean.Weekday, clean.DayPlanType, clean.DayCustomCarbG, clean.DayCustomProteinG, clean.DayCustomFatG)
}

func (s *MealPlanService) RemoveMeal(ctx context.Context, mealID, userID uuid.UUID) error {
	return s.repo.RemoveMeal(ctx, mealID, userID)
}

type OverageError struct {
	Overage meal.Overage
}

func (e OverageError) Error() string {
	var parts []string
	if e.Overage.CarbG > 0 {
		parts = append(parts, fmt.Sprintf("%.0fg carbs", e.Overage.CarbG))
	}
	if e.Overage.ProteinG > 0 {
		parts = append(parts, fmt.Sprintf("%.0fg protein", e.Overage.ProteinG))
	}
	if e.Overage.FatG > 0 {
		parts = append(parts, fmt.Sprintf("%.0fg fat", e.Overage.FatG))
	}
	return fmt.Sprintf("Exceeds day target by %s", strings.Join(parts, ", "))
}

// AddIngredient looks up the ingredient's per-100g profile, snapshots the
// macros for this quantity, and recalculates the meal's and plan's totals.
func (s *MealPlanService) AddIngredient(ctx context.Context, mealID, userID uuid.UUID, in MealIngredientInput) (MealIngredient, error) {
	clean, err := ValidateMealIngredient(in)
	if err != nil {
		return MealIngredient{}, err
	}

	ingredient, err := s.repo.GetIngredient(ctx, clean.IngredientID, userID)
	if err != nil {
		return MealIngredient{}, err
	}

	macros := ingredient.MacrosFor(clean.QuantityGrams)
	return s.repo.AddIngredient(ctx, mealID, userID, clean.IngredientID, clean.QuantityGrams, macros)
}

// AddIngredientChecked adds an ingredient and verifies whether the day's macro target is exceeded.
// If exceeded without confirmOverage, the change is reverted and OverageError is returned.
func (s *MealPlanService) AddIngredientChecked(
	ctx context.Context,
	mealID, userID uuid.UUID,
	in MealIngredientInput,
	confirmOverage bool,
	goals MacroGoalLookup,
) (MealIngredient, *meal.Overage, error) {
	added, err := s.AddIngredient(ctx, mealID, userID, in)
	if err != nil {
		return MealIngredient{}, nil, err
	}

	mealRow, err := s.repo.GetMeal(ctx, mealID, userID)
	if err != nil || mealRow.Weekday == nil {
		return added, nil, nil
	}

	plan, err := s.repo.GetPlan(ctx, mealRow.MealPlanID, userID)
	if err != nil {
		return added, nil, nil
	}

	if plan.PlanType == "" && mealRow.DayPlanType == "" && mealRow.DayCustomCarbG == nil {
		return added, nil, nil
	}

	if goals == nil {
		return added, nil, nil
	}

	macroGoal, err := goals.Current(ctx, userID)
	if err != nil {
		return added, nil, nil
	}

	target := meal.ResolveDayTarget(
		macroGoal.ProteinG,
		macroGoal.FatG,
		macroGoal.CarbG,
		plan.PlanType,
		plan.CustomCarbPct,
		mealRow.DayPlanType,
		mealRow.DayCustomCarbG,
		mealRow.DayCustomProteinG,
		mealRow.DayCustomFatG,
	)

	dayTotals, err := s.repo.SumDayMacros(ctx, mealRow.MealPlanID, *mealRow.Weekday)
	if err != nil {
		return added, nil, nil
	}

	overage := meal.CheckOverage(dayTotals, target)
	if overage.IsOver {
		if !confirmOverage {
			_ = s.RemoveIngredient(ctx, added.ID, userID)
			return MealIngredient{}, &overage, OverageError{Overage: overage}
		}
		_ = s.repo.ConfirmOverage(ctx, mealID)
	}

	return added, &overage, nil
}

// AddIngredients adds several portions to one meal, as a spoken meal arrives.
//
// Every input is validated and every ingredient looked up before the first row
// is written, so a bad line — a missing quantity, an id this account cannot
// see — refuses the whole batch instead of leaving half a meal behind. The
// writes themselves are not one transaction; a database failure part-way is
// reported and whatever was written stays, exactly as it would after the same
// failure on the one-at-a-time form.
func (s *MealPlanService) AddIngredients(ctx context.Context, mealID, userID uuid.UUID, in []MealIngredientInput) ([]MealIngredient, error) {
	if len(in) == 0 {
		return nil, apperr.FieldErrors{}.Add("ingredient_id", "Choose at least one ingredient.").OrNil()
	}

	macros := make([]Macros, len(in))
	for i, line := range in {
		clean, err := ValidateMealIngredient(line)
		if err != nil {
			return nil, err
		}
		ingredient, err := s.repo.GetIngredient(ctx, clean.IngredientID, userID)
		if err != nil {
			return nil, err
		}
		macros[i] = ingredient.MacrosFor(clean.QuantityGrams)
	}

	added := make([]MealIngredient, 0, len(in))
	for i, line := range in {
		row, err := s.repo.AddIngredient(ctx, mealID, userID, line.IngredientID, line.QuantityGrams, macros[i])
		if err != nil {
			return added, err
		}
		added = append(added, row)
	}
	return added, nil
}

func (s *MealPlanService) RemoveIngredient(ctx context.Context, mealIngredientID, userID uuid.UUID) error {
	return s.repo.RemoveIngredient(ctx, mealIngredientID, userID)
}
