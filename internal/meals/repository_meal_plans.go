package meals

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	mealsdb "github.com/NorthAIProject/north-client/internal/meals/db"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// NewPlan is a meal plan's own row, before it has an id.
type NewPlan struct {
	Name          string
	Description   string
	Objective     string
	ActivityLevel string
	Gender        string
	Settings      meal.PlanSettings
}

// NewDay is a day to create with a plan, with any meals already planned for it.
type NewDay struct {
	Weekday time.Weekday
	Meals   []NewMeal
}

// NewMeal is a meal to create, with its portions.
type NewMeal struct {
	Name     string
	Portions []NewPortion
}

// NewPortion is an ingredient at a quantity, with the macros it adds already
// worked out, so the repository never needs the ingredient's profile.
type NewPortion struct {
	IngredientID  uuid.UUID
	QuantityGrams float64
	Macros        Macros
}

// CreatePlan writes a plan, its days and any meals and portions they come
// with, all or nothing.
func (r *Repository) CreatePlan(ctx context.Context, userID uuid.UUID, plan NewPlan, days []NewDay) (uuid.UUID, error) {
	var planID uuid.UUID
	err := r.inTx(ctx, func(q *mealsdb.Queries) error {
		row, err := q.CreateMealPlan(ctx, mealsdb.CreateMealPlanParams{
			UserID: userID, Name: plan.Name, Description: plan.Description, Objective: plan.Objective,
			ActivityLevel: plan.ActivityLevel, Gender: plan.Gender,
			PlanType: string(plan.Settings.Type), CustomCarbPct: plan.Settings.CustomCarbPct, Mode: string(plan.Settings.Mode),
		})
		if err != nil {
			return apperr.Wrap(err, "create meal plan")
		}
		planID = row.ID
		tx := &PlanTx{q: q}
		for _, d := range days {
			day, err := tx.AddDay(ctx, planID, d.Weekday)
			if err != nil {
				return err
			}
			for _, m := range d.Meals {
				created, err := tx.AddMeal(ctx, planID, day.ID, m.Name)
				if err != nil {
					return err
				}
				if _, err := tx.AddPortions(ctx, planID, created.ID, m.Portions); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return planID, err
}

// GetPlan loads a plan with its days, their meals and each meal's
// ingredients. A meal plan realistically has a handful of meals, so the one
// query per meal here is not the hot path the coach's per-message reads are.
func (r *Repository) GetPlan(ctx context.Context, id, userID uuid.UUID) (MealPlan, error) {
	row, err := r.q.GetMealPlan(ctx, mealsdb.GetMealPlanParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MealPlan{}, apperr.ErrNotFound
		}
		return MealPlan{}, apperr.Wrap(err, "get meal plan")
	}
	return loadPlan(ctx, r.q, row, true)
}

// ListPlans is the index view: plans with their days, but not the days' meals.
func (r *Repository) ListPlans(ctx context.Context, userID uuid.UUID) ([]MealPlan, error) {
	rows, err := r.q.ListMealPlans(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "list meal plans")
	}
	dayRows, err := r.q.ListMealPlanDaysByUser(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "list meal plan days")
	}
	days := map[uuid.UUID][]meal.Day{}
	for _, d := range dayRows {
		days[d.MealPlanID] = append(days[d.MealPlanID], dayFromDB(d))
	}
	out := make([]MealPlan, 0, len(rows))
	for _, row := range rows {
		plan := mealPlanFromDB(row)
		plan.Days = days[row.ID]
		out = append(out, plan)
	}
	return out, nil
}

func (r *Repository) DeletePlan(ctx context.Context, id, userID uuid.UUID) error {
	return apperr.Wrap(r.q.DeleteMealPlan(ctx, mealsdb.DeleteMealPlanParams{ID: id, UserID: userID}), "delete meal plan")
}

// WithPlanLocked runs fn in one transaction that holds the plan's row lock,
// passing it the plan as stored: its days and their meals with totals, but
// not the meals' ingredients. Changes fn makes through tx commit together, or
// not at all if fn returns an error. apperr.ErrNotFound if the plan is not
// the user's.
func (r *Repository) WithPlanLocked(ctx context.Context, planID, userID uuid.UUID, fn func(tx *PlanTx, plan MealPlan) error) error {
	return r.inTx(ctx, func(q *mealsdb.Queries) error {
		row, err := q.LockMealPlan(ctx, mealsdb.LockMealPlanParams{ID: planID, UserID: userID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apperr.ErrNotFound
			}
			return apperr.Wrap(err, "lock meal plan")
		}
		plan, err := loadPlan(ctx, q, row, false)
		if err != nil {
			return err
		}
		return fn(&PlanTx{q: q}, plan)
	})
}

// PlanTx writes to a plan inside WithPlanLocked's transaction. It persists
// and keeps totals current; whether a change is allowed is the service's call.
type PlanTx struct {
	q *mealsdb.Queries
}

func (tx *PlanTx) UpdateSettings(ctx context.Context, planID uuid.UUID, name, description string, s meal.PlanSettings) error {
	return apperr.Wrap(tx.q.UpdateMealPlanSettings(ctx, mealsdb.UpdateMealPlanSettingsParams{
		ID: planID, Name: name, Description: description,
		PlanType: string(s.Type), CustomCarbPct: s.CustomCarbPct, Mode: string(s.Mode),
	}), "update meal plan settings")
}

// ClearOverrides returns every day of the plan to the plan's default target.
func (tx *PlanTx) ClearOverrides(ctx context.Context, planID uuid.UUID) error {
	return apperr.Wrap(tx.q.ClearMealPlanDayOverrides(ctx, planID), "clear day overrides")
}

func (tx *PlanTx) AddDay(ctx context.Context, planID uuid.UUID, weekday time.Weekday) (meal.Day, error) {
	row, err := tx.q.CreateMealPlanDay(ctx, mealsdb.CreateMealPlanDayParams{MealPlanID: planID, Weekday: int16(weekday)})
	if err != nil {
		return meal.Day{}, apperr.Wrap(err, "create meal plan day")
	}
	return dayFromDB(row), nil
}

func (tx *PlanTx) UpdateDay(ctx context.Context, dayID uuid.UUID, o meal.DayOverride) error {
	var carbType *string
	if o.CarbType != nil {
		s := string(*o.CarbType)
		carbType = &s
	}
	return apperr.Wrap(tx.q.UpdateMealPlanDay(ctx, mealsdb.UpdateMealPlanDayParams{
		ID: dayID, CarbType: carbType, CarbG: o.CarbG, ProteinG: o.ProteinG, FatG: o.FatG,
	}), "update meal plan day")
}

// RemoveDay deletes a day with its meals.
func (tx *PlanTx) RemoveDay(ctx context.Context, planID, dayID uuid.UUID) error {
	if err := tx.q.DeleteMealPlanDay(ctx, dayID); err != nil {
		return apperr.Wrap(err, "delete meal plan day")
	}
	return recalculatePlanTotals(ctx, tx.q, planID)
}

func (tx *PlanTx) AddMeal(ctx context.Context, planID, dayID uuid.UUID, name string) (Meal, error) {
	row, err := tx.q.CreateMeal(ctx, mealsdb.CreateMealParams{MealPlanID: planID, DayID: dayID, Name: name})
	if err != nil {
		return Meal{}, apperr.Wrap(err, "create meal")
	}
	return mealFromDB(row), nil
}

// AddPortions adds ingredients to a meal and recalculates the meal's and the
// plan's totals.
func (tx *PlanTx) AddPortions(ctx context.Context, planID, mealID uuid.UUID, portions []NewPortion) ([]MealIngredient, error) {
	if len(portions) == 0 {
		return nil, nil
	}
	added := make([]MealIngredient, 0, len(portions))
	for _, p := range portions {
		row, err := tx.q.CreateMealIngredient(ctx, mealsdb.CreateMealIngredientParams{
			MealID: mealID, IngredientID: p.IngredientID, QuantityGrams: p.QuantityGrams,
			Calories: p.Macros.Calories, ProteinG: p.Macros.ProteinG, FatG: p.Macros.FatG, CarbsG: p.Macros.CarbG,
		})
		if err != nil {
			return nil, apperr.Wrap(err, "create meal ingredient")
		}
		added = append(added, mealIngredientFromDB(row))
	}
	if err := recalculateTotals(ctx, tx.q, mealID, planID); err != nil {
		return nil, apperr.Wrap(err, "recalculate totals after adding ingredients")
	}
	return added, nil
}

// PlanIDOfMeal resolves the plan a meal belongs to, so a change to the meal
// can lock that plan. apperr.ErrNotFound if the meal is not the user's.
func (r *Repository) PlanIDOfMeal(ctx context.Context, mealID, userID uuid.UUID) (uuid.UUID, error) {
	m, err := r.GetMeal(ctx, mealID, userID)
	return m.MealPlanID, err
}

// PlanIDOfDay resolves the plan a day belongs to. apperr.ErrNotFound if the
// day is not the user's.
func (r *Repository) PlanIDOfDay(ctx context.Context, dayID, userID uuid.UUID) (uuid.UUID, error) {
	row, err := r.q.GetMealPlanDayOwned(ctx, mealsdb.GetMealPlanDayOwnedParams{ID: dayID, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, apperr.ErrNotFound
		}
		return uuid.Nil, apperr.Wrap(err, "get meal plan day")
	}
	return row.MealPlanID, nil
}

// PlanIDOfMealIngredient resolves the plan a meal's ingredient belongs to.
// apperr.ErrNotFound if it is not the user's.
func (r *Repository) PlanIDOfMealIngredient(ctx context.Context, mealIngredientID, userID uuid.UUID) (uuid.UUID, error) {
	owned, err := r.q.GetMealIngredientOwned(ctx, mealsdb.GetMealIngredientOwnedParams{ID: mealIngredientID, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, apperr.ErrNotFound
		}
		return uuid.Nil, apperr.Wrap(err, "get meal ingredient")
	}
	return r.PlanIDOfMeal(ctx, owned.OwnedMealID, userID)
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
	m, err := r.GetMeal(ctx, mealID, userID)
	if err != nil {
		return err
	}
	return r.inTx(ctx, func(q *mealsdb.Queries) error {
		if err := q.DeleteMealOwned(ctx, mealsdb.DeleteMealOwnedParams{ID: mealID, UserID: userID}); err != nil {
			return apperr.Wrap(err, "delete meal")
		}
		return apperr.Wrap(recalculatePlanTotals(ctx, q, m.MealPlanID), "recalculate plan totals after removing meal")
	})
}

func (r *Repository) RemoveIngredient(ctx context.Context, mealIngredientID, userID uuid.UUID) error {
	owned, err := r.q.GetMealIngredientOwned(ctx, mealsdb.GetMealIngredientOwnedParams{ID: mealIngredientID, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.ErrNotFound
		}
		return apperr.Wrap(err, "get meal ingredient")
	}
	m, err := r.GetMeal(ctx, owned.OwnedMealID, userID)
	if err != nil {
		return err
	}
	return r.inTx(ctx, func(q *mealsdb.Queries) error {
		if err := q.DeleteMealIngredient(ctx, mealIngredientID); err != nil {
			return apperr.Wrap(err, "delete meal ingredient")
		}
		return apperr.Wrap(recalculateTotals(ctx, q, owned.OwnedMealID, m.MealPlanID), "recalculate totals after removing ingredient")
	})
}

// inTx runs fn in a transaction, committing only if it returns nil.
func (r *Repository) inTx(ctx context.Context, fn func(q *mealsdb.Queries) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return apperr.Wrap(err, "begin transaction")
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(r.q.WithTx(tx)); err != nil {
		return err
	}
	return apperr.Wrap(tx.Commit(ctx), "commit transaction")
}

// loadPlan fills a plan row in with its days and their meals, and with each
// meal's ingredients when withIngredients is set.
func loadPlan(ctx context.Context, q *mealsdb.Queries, row mealsdb.MealPlan, withIngredients bool) (MealPlan, error) {
	plan := mealPlanFromDB(row)

	dayRows, err := q.ListMealPlanDays(ctx, row.ID)
	if err != nil {
		return MealPlan{}, apperr.Wrap(err, "list meal plan days")
	}
	plan.Days = make([]meal.Day, len(dayRows))
	byID := make(map[uuid.UUID]int, len(dayRows))
	for i, d := range dayRows {
		plan.Days[i] = dayFromDB(d)
		byID[d.ID] = i
	}

	mealRows, err := q.ListMealsByPlan(ctx, row.ID)
	if err != nil {
		return MealPlan{}, apperr.Wrap(err, "list meals by plan")
	}
	for _, mealRow := range mealRows {
		m := mealFromDB(mealRow)
		if withIngredients {
			ingredientRows, err := q.ListMealIngredients(ctx, m.ID)
			if err != nil {
				return MealPlan{}, apperr.Wrap(err, "list meal ingredients")
			}
			m.Ingredients = make([]MealIngredient, 0, len(ingredientRows))
			for _, ingredientRow := range ingredientRows {
				m.Ingredients = append(m.Ingredients, mealIngredientFromListRow(ingredientRow))
			}
		}
		i := byID[m.DayID]
		plan.Days[i].Meals = append(plan.Days[i].Meals, m)
	}
	return plan, nil
}

// recalculateTotals sums a meal's ingredients into its total_macros, then
// sums the plan's meals into the plan's total_macros. Ports FitMe's
// calculateTotals two levels deep; callers run it in their transaction so a
// reader never sees a meal and plan total momentarily out of sync.
func recalculateTotals(ctx context.Context, q *mealsdb.Queries, mealID, planID uuid.UUID) error {
	sums, err := q.SumMealIngredientMacros(ctx, mealID)
	if err != nil {
		return apperr.Wrap(err, "sum meal ingredients")
	}
	mealMacros := Macros{Calories: sums.Calories, ProteinG: sums.ProteinG, FatG: sums.FatG, CarbG: sums.CarbsG}
	if err = q.UpdateMealTotalMacros(ctx, mealsdb.UpdateMealTotalMacrosParams{ID: mealID, TotalMacros: macrosToJSON(mealMacros)}); err != nil {
		return apperr.Wrap(err, "update meal total macros")
	}
	return recalculatePlanTotals(ctx, q, planID)
}

// recalculatePlanTotals re-sums a plan's meals without touching any single
// meal's own total — used after a meal or day is removed, where there is no
// meal left to recompute.
func recalculatePlanTotals(ctx context.Context, q *mealsdb.Queries, planID uuid.UUID) error {
	siblings, err := q.ListMealsByPlan(ctx, planID)
	if err != nil {
		return apperr.Wrap(err, "list meals by plan")
	}
	var planTotal Macros
	for _, sibling := range siblings {
		planTotal = planTotal.Add(macrosFromJSON(sibling.TotalMacros))
	}
	return apperr.Wrap(q.UpdateMealPlanTotalMacros(ctx, mealsdb.UpdateMealPlanTotalMacrosParams{ID: planID, TotalMacros: macrosToJSON(planTotal)}), "update plan total macros")
}

func mealPlanFromDB(row mealsdb.MealPlan) MealPlan {
	return MealPlan{
		ID:            row.ID,
		UserID:        row.UserID,
		Name:          row.Name,
		Description:   row.Description,
		Objective:     row.Objective,
		ActivityLevel: row.ActivityLevel,
		Gender:        row.Gender,
		Settings: meal.PlanSettings{
			Type: meal.PlanType(row.PlanType), CustomCarbPct: row.CustomCarbPct, Mode: meal.Mode(row.Mode),
		},
		TotalMacros: macrosFromJSON(row.TotalMacros),
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
}

func dayFromDB(row mealsdb.MealPlanDay) meal.Day {
	d := meal.Day{
		ID: row.ID, PlanID: row.MealPlanID, Weekday: time.Weekday(row.Weekday),
		Override: meal.DayOverride{CarbG: row.CarbG, ProteinG: row.ProteinG, FatG: row.FatG},
	}
	if row.CarbType != nil {
		t := meal.PlanType(*row.CarbType)
		d.Override.CarbType = &t
	}
	return d
}

func mealFromDB(row mealsdb.Meal) Meal {
	return Meal{
		ID: row.ID, MealPlanID: row.MealPlanID, DayID: row.DayID, MealNumber: int(row.MealNumber), Name: row.Name,
		TotalMacros: macrosFromJSON(row.TotalMacros),
		CreatedAt:   row.CreatedAt,
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
