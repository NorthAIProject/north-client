package meals

import (
	"context"
	"time"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type FoodLogService struct {
	repo *Repository
}

func NewFoodLogService(repo *Repository) *FoodLogService {
	return &FoodLogService{repo: repo}
}

type LogMealInput struct {
	MealID  uuid.UUID
	LogDate time.Time
}

type LogIngredientInput struct {
	IngredientID  uuid.UUID
	QuantityGrams float64
	LogDate       time.Time
}

// LogMeal records that the user ate a meal from one of their plans today (or
// on LogDate), snapshotting the meal's current total macros.
func (s *FoodLogService) LogMeal(ctx context.Context, userID uuid.UUID, in LogMealInput) (FoodLogEntry, error) {
	if in.LogDate.IsZero() {
		in.LogDate = time.Now()
	}

	meal, err := s.repo.GetMeal(ctx, in.MealID, userID)
	if err != nil {
		return FoodLogEntry{}, err
	}

	return s.repo.InsertFoodLog(ctx, userID, in.LogDate, &meal.ID, nil, nil, meal.Name, meal.TotalMacros)
}

// LogIngredient records an ad-hoc ingredient + quantity not tied to any meal
// plan, snapshotting the macros for that quantity.
func (s *FoodLogService) LogIngredient(ctx context.Context, userID uuid.UUID, in LogIngredientInput) (FoodLogEntry, error) {
	if in.LogDate.IsZero() {
		in.LogDate = time.Now()
	}
	if in.QuantityGrams <= 0 {
		return FoodLogEntry{}, apperr.Wrap(apperr.ErrValidation, "enter a quantity greater than zero")
	}

	ingredient, err := s.repo.GetIngredient(ctx, in.IngredientID)
	if err != nil {
		return FoodLogEntry{}, err
	}

	macros := ingredient.MacrosFor(in.QuantityGrams)
	qty := in.QuantityGrams
	return s.repo.InsertFoodLog(ctx, userID, in.LogDate, nil, &ingredient.ID, &qty, ingredient.Name, macros)
}

func (s *FoodLogService) Delete(ctx context.Context, id, userID uuid.UUID) error {
	return s.repo.DeleteFoodLog(ctx, id, userID)
}

func (s *FoodLogService) Day(ctx context.Context, userID uuid.UUID, date time.Time) ([]FoodLogEntry, error) {
	return s.repo.FoodLogsByDate(ctx, userID, date)
}

func (s *FoodLogService) Range(ctx context.Context, userID uuid.UUID, from, to time.Time) ([]FoodLogEntry, error) {
	return s.repo.FoodLogsByRange(ctx, userID, from, to)
}

func (s *FoodLogService) DailyTotals(ctx context.Context, userID uuid.UUID, date time.Time) (Macros, error) {
	return s.repo.DailyTotals(ctx, userID, date)
}
