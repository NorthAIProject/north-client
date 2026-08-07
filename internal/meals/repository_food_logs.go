package meals

import (
	"context"
	"time"

	"github.com/google/uuid"

	mealsdb "github.com/NorthAIProject/north-client/internal/meals/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func (r *Repository) InsertFoodLog(ctx context.Context, userID uuid.UUID, logDate time.Time, mealID, ingredientID *uuid.UUID, quantityGrams *float64, label string, macros Macros) (FoodLogEntry, error) {
	row, err := r.q.InsertFoodLog(ctx, mealsdb.InsertFoodLogParams{
		UserID: userID, LogDate: toDate(logDate),
		MealID: mealID, IngredientID: ingredientID, QuantityGrams: quantityGrams,
		Label: label, Calories: macros.Calories, ProteinG: macros.ProteinG, FatG: macros.FatG, CarbsG: macros.CarbG,
	})
	if err != nil {
		return FoodLogEntry{}, apperr.Wrap(err, "insert food log")
	}
	return foodLogFromDB(row), nil
}

func (r *Repository) DeleteFoodLog(ctx context.Context, id, userID uuid.UUID) error {
	return apperr.Wrap(r.q.DeleteFoodLog(ctx, mealsdb.DeleteFoodLogParams{ID: id, UserID: userID}), "delete food log")
}

func (r *Repository) FoodLogsByDate(ctx context.Context, userID uuid.UUID, date time.Time) ([]FoodLogEntry, error) {
	rows, err := r.q.ListFoodLogsByDate(ctx, mealsdb.ListFoodLogsByDateParams{UserID: userID, LogDate: toDate(date)})
	if err != nil {
		return nil, apperr.Wrap(err, "list food logs by date")
	}
	return foodLogsFromDB(rows), nil
}

func (r *Repository) FoodLogsByRange(ctx context.Context, userID uuid.UUID, from, to time.Time) ([]FoodLogEntry, error) {
	rows, err := r.q.ListFoodLogsByRange(ctx, mealsdb.ListFoodLogsByRangeParams{UserID: userID, LogDate: toDate(from), LogDate_2: toDate(to)})
	if err != nil {
		return nil, apperr.Wrap(err, "list food logs by range")
	}
	return foodLogsFromDB(rows), nil
}

func (r *Repository) DailyTotals(ctx context.Context, userID uuid.UUID, date time.Time) (Macros, error) {
	row, err := r.q.DailyFoodLogTotals(ctx, mealsdb.DailyFoodLogTotalsParams{UserID: userID, LogDate: toDate(date)})
	if err != nil {
		return Macros{}, apperr.Wrap(err, "daily food log totals")
	}
	return Macros{Calories: row.Calories, ProteinG: row.ProteinG, FatG: row.FatG, CarbG: row.CarbsG}, nil
}

func foodLogsFromDB(rows []mealsdb.FoodLog) []FoodLogEntry {
	out := make([]FoodLogEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, foodLogFromDB(row))
	}
	return out
}

func foodLogFromDB(row mealsdb.FoodLog) FoodLogEntry {
	return FoodLogEntry{
		ID: row.ID, UserID: row.UserID, LogDate: fromDate(row.LogDate),
		MealID: row.MealID, IngredientID: row.IngredientID, Label: row.Label,
		QuantityGrams: row.QuantityGrams,
		Macros:        Macros{Calories: row.Calories, ProteinG: row.ProteinG, FatG: row.FatG, CarbG: row.CarbsG},
		LoggedAt:      row.LoggedAt,
	}
}
