package meals

import (
	"context"

	"github.com/google/uuid"

	mealsdb "github.com/NorthAIProject/north-client/internal/meals/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// CreateImportedPlan writes a whole imported plan — personal ingredients, the
// plan, its meals, their portions and every total — in one transaction.
//
// One transaction because an import is one decision. A plan with Monday and no
// Tuesday, left behind by a failure half-way, is a plan nobody chose.
func (r *Repository) CreateImportedPlan(ctx context.Context, userID uuid.UUID, plan MealPlanInput, meals []importMealRow) (MealPlan, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return MealPlan{}, apperr.Wrap(err, "begin import transaction")
	}
	defer func() { _ = tx.Rollback(ctx) }()

	qtx := r.q.WithTx(tx)

	planRow, err := qtx.CreateMealPlan(ctx, mealsdb.CreateMealPlanParams{
		UserID:        userID,
		Name:          plan.Name,
		PlanType:      stringPtr(plan.PlanType),
		CustomCarbPct: plan.CustomCarbPct,
		MacroPlanID:   plan.MacroPlanID,
	})
	if err != nil {
		return MealPlan{}, apperr.Wrap(err, "create imported meal plan")
	}

	var planTotal Macros
	for _, m := range meals {
		mealRow, err := qtx.CreateMeal(ctx, mealsdb.CreateMealParams{
			MealPlanID: planRow.ID,
			MealNumber: int16(m.input.MealNumber),
			Name:       m.input.Name,
			Weekday:    int16Ptr(m.input.Weekday),
		})
		if err != nil {
			return MealPlan{}, apperr.Wrap(err, "create imported meal")
		}

		for _, item := range m.items {
			ingredientID := item.ingredientID
			if item.newIngredient != nil {
				ing := item.newIngredient
				created, err := qtx.CreateIngredient(ctx, mealsdb.CreateIngredientParams{
					UserID:           &userID,
					Name:             ing.Name,
					Brand:            ing.Brand,
					Category:         ing.Category,
					ServingSizeGrams: ing.ServingSizeGrams,
					CaloriesPer100g:  ing.Per100g.Calories,
					ProteinGPer100g:  ing.Per100g.ProteinG,
					FatGPer100g:      ing.Per100g.FatG,
					CarbsGPer100g:    ing.Per100g.CarbG,
				})
				if err != nil {
					return MealPlan{}, apperr.Wrap(err, "create imported ingredient")
				}
				ingredientID = created.ID
			}

			if _, err := qtx.CreateMealIngredient(ctx, mealsdb.CreateMealIngredientParams{
				MealID: mealRow.ID, IngredientID: ingredientID, QuantityGrams: item.grams,
				Calories: item.macros.Calories, ProteinG: item.macros.ProteinG, FatG: item.macros.FatG, CarbsG: item.macros.CarbG,
			}); err != nil {
				return MealPlan{}, apperr.Wrap(err, "create imported meal ingredient")
			}
		}

		if err := qtx.UpdateMealTotalMacros(ctx, mealsdb.UpdateMealTotalMacrosParams{ID: mealRow.ID, TotalMacros: macrosToJSON(m.total)}); err != nil {
			return MealPlan{}, apperr.Wrap(err, "update imported meal totals")
		}
		if m.overConfirmed {
			if err := qtx.ConfirmMealOverage(ctx, mealRow.ID); err != nil {
				return MealPlan{}, apperr.Wrap(err, "confirm imported meal overage")
			}
		}
		planTotal = planTotal.Add(m.total)
	}

	if err := qtx.UpdateMealPlanTotalMacros(ctx, mealsdb.UpdateMealPlanTotalMacrosParams{ID: planRow.ID, TotalMacros: macrosToJSON(planTotal)}); err != nil {
		return MealPlan{}, apperr.Wrap(err, "update imported plan totals")
	}
	if err := tx.Commit(ctx); err != nil {
		return MealPlan{}, apperr.Wrap(err, "commit import transaction")
	}

	return r.GetPlan(ctx, planRow.ID, userID)
}
