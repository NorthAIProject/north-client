package meals

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	mealsdb "github.com/NorthAIProject/north-client/internal/meals/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func (r *Repository) CreatePlan(ctx context.Context, userID uuid.UUID, name, description, objective, activityLevel, gender string) (MealPlan, error) {
	row, err := r.q.CreateMealPlan(ctx, mealsdb.CreateMealPlanParams{
		UserID: userID, Name: name, Description: description,
		Objective: objective, ActivityLevel: activityLevel, Gender: gender,
	})
	if err != nil {
		return MealPlan{}, apperr.Wrap(err, "create meal plan")
	}
	return mealPlanFromDB(row), nil
}

// GetPlan loads a plan with its meals and each meal's ingredients. A meal
// plan realistically has a handful of meals, so the one query per meal here
// is not the hot path the coach's per-message reads are.
func (r *Repository) GetPlan(ctx context.Context, id, userID uuid.UUID) (MealPlan, error) {
	row, err := r.q.GetMealPlan(ctx, mealsdb.GetMealPlanParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MealPlan{}, apperr.ErrNotFound
		}
		return MealPlan{}, apperr.Wrap(err, "get meal plan")
	}

	plan := mealPlanFromDB(row)

	mealRows, err := r.q.ListMealsByPlan(ctx, id)
	if err != nil {
		return MealPlan{}, apperr.Wrap(err, "list meals by plan")
	}

	plan.Meals = make([]Meal, 0, len(mealRows))
	for _, mealRow := range mealRows {
		meal := mealFromDB(mealRow)

		ingredientRows, err := r.q.ListMealIngredients(ctx, meal.ID)
		if err != nil {
			return MealPlan{}, apperr.Wrap(err, "list meal ingredients")
		}
		meal.Ingredients = make([]MealIngredient, 0, len(ingredientRows))
		for _, ingredientRow := range ingredientRows {
			meal.Ingredients = append(meal.Ingredients, mealIngredientFromListRow(ingredientRow))
		}

		plan.Meals = append(plan.Meals, meal)
	}

	return plan, nil
}

// ListPlans is the index view: plans without their nested meals.
func (r *Repository) ListPlans(ctx context.Context, userID uuid.UUID) ([]MealPlan, error) {
	rows, err := r.q.ListMealPlans(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "list meal plans")
	}
	out := make([]MealPlan, 0, len(rows))
	for _, row := range rows {
		out = append(out, mealPlanFromDB(row))
	}
	return out, nil
}

func (r *Repository) UpdatePlan(ctx context.Context, id, userID uuid.UUID, name, description, objective, activityLevel, gender string) (MealPlan, error) {
	row, err := r.q.UpdateMealPlan(ctx, mealsdb.UpdateMealPlanParams{
		ID: id, UserID: userID, Name: name, Description: description,
		Objective: objective, ActivityLevel: activityLevel, Gender: gender,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MealPlan{}, apperr.ErrNotFound
		}
		return MealPlan{}, apperr.Wrap(err, "update meal plan")
	}
	return mealPlanFromDB(row), nil
}

func (r *Repository) DeletePlan(ctx context.Context, id, userID uuid.UUID) error {
	return apperr.Wrap(r.q.DeleteMealPlan(ctx, mealsdb.DeleteMealPlanParams{ID: id, UserID: userID}), "delete meal plan")
}

func (r *Repository) AddMeal(ctx context.Context, planID, userID uuid.UUID, name string, mealNumber int) (Meal, error) {
	// Ownership check: a meal cannot be created under a plan that is not the
	// caller's, so confirm the plan resolves for this user first.
	if _, err := r.q.GetMealPlan(ctx, mealsdb.GetMealPlanParams{ID: planID, UserID: userID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Meal{}, apperr.ErrNotFound
		}
		return Meal{}, apperr.Wrap(err, "get meal plan")
	}

	row, err := r.q.CreateMeal(ctx, mealsdb.CreateMealParams{MealPlanID: planID, MealNumber: int16(mealNumber), Name: name})
	if err != nil {
		return Meal{}, apperr.Wrap(err, "create meal")
	}
	return mealFromDB(row), nil
}

// GetMeal loads a single meal, checking ownership via its parent plan.
// Exported for FoodLogService, which needs a meal's name and total macros to
// snapshot a log entry.
func (r *Repository) GetMeal(ctx context.Context, mealID, userID uuid.UUID) (Meal, error) {
	row, err := r.q.GetMealOwned(ctx, mealsdb.GetMealOwnedParams{ID: mealID, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Meal{}, apperr.ErrNotFound
		}
		return Meal{}, apperr.Wrap(err, "get meal")
	}
	return mealFromDB(row), nil
}

func (r *Repository) RemoveMeal(ctx context.Context, mealID, userID uuid.UUID) error {
	meal, err := r.q.GetMealOwned(ctx, mealsdb.GetMealOwnedParams{ID: mealID, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.ErrNotFound
		}
		return apperr.Wrap(err, "get meal")
	}

	if err := r.q.DeleteMealOwned(ctx, mealsdb.DeleteMealOwnedParams{ID: mealID, UserID: userID}); err != nil {
		return apperr.Wrap(err, "delete meal")
	}

	return apperr.Wrap(r.recalculatePlanTotals(ctx, meal.MealPlanID), "recalculate plan totals after removing meal")
}

func (r *Repository) AddIngredient(ctx context.Context, mealID, userID uuid.UUID, ingredientID uuid.UUID, quantityGrams float64, macros Macros) (MealIngredient, error) {
	meal, err := r.q.GetMealOwned(ctx, mealsdb.GetMealOwnedParams{ID: mealID, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MealIngredient{}, apperr.ErrNotFound
		}
		return MealIngredient{}, apperr.Wrap(err, "get meal")
	}

	row, err := r.q.CreateMealIngredient(ctx, mealsdb.CreateMealIngredientParams{
		MealID: mealID, IngredientID: ingredientID, QuantityGrams: quantityGrams,
		Calories: macros.Calories, ProteinG: macros.ProteinG, FatG: macros.FatG, CarbsG: macros.CarbG,
	})
	if err != nil {
		return MealIngredient{}, apperr.Wrap(err, "create meal ingredient")
	}

	if err := r.recalculateTotals(ctx, mealID, meal.MealPlanID); err != nil {
		return MealIngredient{}, apperr.Wrap(err, "recalculate totals after adding ingredient")
	}

	return mealIngredientFromDB(row), nil
}

func (r *Repository) RemoveIngredient(ctx context.Context, mealIngredientID, userID uuid.UUID) error {
	owned, err := r.q.GetMealIngredientOwned(ctx, mealsdb.GetMealIngredientOwnedParams{ID: mealIngredientID, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.ErrNotFound
		}
		return apperr.Wrap(err, "get meal ingredient")
	}

	if err := r.q.DeleteMealIngredient(ctx, mealIngredientID); err != nil {
		return apperr.Wrap(err, "delete meal ingredient")
	}

	meal, err := r.q.GetMealOwned(ctx, mealsdb.GetMealOwnedParams{ID: owned.OwnedMealID, UserID: userID})
	if err != nil {
		return apperr.Wrap(err, "get meal after removing ingredient")
	}

	return apperr.Wrap(r.recalculateTotals(ctx, owned.OwnedMealID, meal.MealPlanID), "recalculate totals after removing ingredient")
}

// recalculateTotals sums a meal's ingredients into its total_macros, then
// sums the plan's meals into the plan's total_macros. Ports FitMe's
// calculateTotals two levels deep, in a transaction so a reader never sees a
// meal and plan total momentarily out of sync.
func (r *Repository) recalculateTotals(ctx context.Context, mealID, planID uuid.UUID) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return apperr.Wrap(err, "begin recalculate transaction")
	}
	defer tx.Rollback(ctx)

	qtx := r.q.WithTx(tx)

	sums, err := qtx.SumMealIngredientMacros(ctx, mealID)
	if err != nil {
		return apperr.Wrap(err, "sum meal ingredients")
	}
	mealMacros := Macros{Calories: sums.Calories, ProteinG: sums.ProteinG, FatG: sums.FatG, CarbG: sums.CarbsG}
	if err := qtx.UpdateMealTotalMacros(ctx, mealsdb.UpdateMealTotalMacrosParams{ID: mealID, TotalMacros: macrosToJSON(mealMacros)}); err != nil {
		return apperr.Wrap(err, "update meal total macros")
	}

	siblings, err := qtx.ListMealsByPlan(ctx, planID)
	if err != nil {
		return apperr.Wrap(err, "list meals by plan")
	}
	var planTotal Macros
	for _, sibling := range siblings {
		if sibling.ID == mealID {
			planTotal = planTotal.Add(mealMacros)
			continue
		}
		planTotal = planTotal.Add(macrosFromJSON(sibling.TotalMacros))
	}
	if err := qtx.UpdateMealPlanTotalMacros(ctx, mealsdb.UpdateMealPlanTotalMacrosParams{ID: planID, TotalMacros: macrosToJSON(planTotal)}); err != nil {
		return apperr.Wrap(err, "update plan total macros")
	}

	return apperr.Wrap(tx.Commit(ctx), "commit recalculate transaction")
}

// recalculatePlanTotals re-sums a plan's meals without touching any single
// meal's own total — used after a whole meal is removed, where there is no
// meal left to recompute.
func (r *Repository) recalculatePlanTotals(ctx context.Context, planID uuid.UUID) error {
	siblings, err := r.q.ListMealsByPlan(ctx, planID)
	if err != nil {
		return apperr.Wrap(err, "list meals by plan")
	}
	var planTotal Macros
	for _, sibling := range siblings {
		planTotal = planTotal.Add(macrosFromJSON(sibling.TotalMacros))
	}
	return apperr.Wrap(r.q.UpdateMealPlanTotalMacros(ctx, mealsdb.UpdateMealPlanTotalMacrosParams{ID: planID, TotalMacros: macrosToJSON(planTotal)}), "update plan total macros")
}

func mealPlanFromDB(row mealsdb.MealPlan) MealPlan {
	return MealPlan{
		ID: row.ID, UserID: row.UserID, Name: row.Name, Description: row.Description,
		Objective: row.Objective, ActivityLevel: row.ActivityLevel, Gender: row.Gender,
		TotalMacros: macrosFromJSON(row.TotalMacros),
		CreatedAt:   row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func mealFromDB(row mealsdb.Meal) Meal {
	return Meal{
		ID: row.ID, MealPlanID: row.MealPlanID, MealNumber: int(row.MealNumber), Name: row.Name,
		TotalMacros: macrosFromJSON(row.TotalMacros), CreatedAt: row.CreatedAt,
	}
}

func mealIngredientFromDB(row mealsdb.MealIngredient) MealIngredient {
	return MealIngredient{
		ID: row.ID, MealID: row.MealID, IngredientID: row.IngredientID,
		QuantityGrams: row.QuantityGrams,
		Macros:        Macros{Calories: row.Calories, ProteinG: row.ProteinG, FatG: row.FatG, CarbG: row.CarbsG},
		CreatedAt:     row.CreatedAt,
	}
}

func mealIngredientFromListRow(row mealsdb.ListMealIngredientsRow) MealIngredient {
	return MealIngredient{
		ID: row.ID, MealID: row.MealID, IngredientID: row.IngredientID,
		IngredientName: row.IngredientName,
		QuantityGrams:  row.QuantityGrams,
		Macros:         Macros{Calories: row.Calories, ProteinG: row.ProteinG, FatG: row.FatG, CarbG: row.CarbsG},
		CreatedAt:      row.CreatedAt,
	}
}
