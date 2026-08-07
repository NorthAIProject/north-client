package meals

import (
	"context"
	"strings"

	"github.com/google/uuid"

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

	return in, errs.OrNil()
}

type MealInput struct {
	Name       string
	MealNumber int
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
	return s.repo.CreatePlan(ctx, userID, clean.Name, clean.Description, clean.Objective, clean.ActivityLevel, clean.Gender)
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
	return s.repo.UpdatePlan(ctx, id, userID, clean.Name, clean.Description, clean.Objective, clean.ActivityLevel, clean.Gender)
}

func (s *MealPlanService) DeletePlan(ctx context.Context, id, userID uuid.UUID) error {
	return s.repo.DeletePlan(ctx, id, userID)
}

func (s *MealPlanService) AddMeal(ctx context.Context, planID, userID uuid.UUID, in MealInput) (Meal, error) {
	clean, err := ValidateMeal(in)
	if err != nil {
		return Meal{}, err
	}
	return s.repo.AddMeal(ctx, planID, userID, clean.Name, clean.MealNumber)
}

func (s *MealPlanService) RemoveMeal(ctx context.Context, mealID, userID uuid.UUID) error {
	return s.repo.RemoveMeal(ctx, mealID, userID)
}

// AddIngredient looks up the ingredient's per-100g profile, snapshots the
// macros for this quantity, and recalculates the meal's and plan's totals.
func (s *MealPlanService) AddIngredient(ctx context.Context, mealID, userID uuid.UUID, in MealIngredientInput) (MealIngredient, error) {
	clean, err := ValidateMealIngredient(in)
	if err != nil {
		return MealIngredient{}, err
	}

	ingredient, err := s.repo.GetIngredient(ctx, clean.IngredientID)
	if err != nil {
		return MealIngredient{}, err
	}

	macros := ingredient.MacrosFor(clean.QuantityGrams)
	return s.repo.AddIngredient(ctx, mealID, userID, clean.IngredientID, clean.QuantityGrams, macros)
}

func (s *MealPlanService) RemoveIngredient(ctx context.Context, mealIngredientID, userID uuid.UUID) error {
	return s.repo.RemoveIngredient(ctx, mealIngredientID, userID)
}
