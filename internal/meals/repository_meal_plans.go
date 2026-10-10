package meals

import (
	"context"
	"errors"
	"slices"
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
	Notes         string
}

// NewDay is a day to create with a plan, with any meals already planned for it.
type NewDay struct {
	Weekday time.Weekday
	Meals   []NewMeal
}

// NewMeal is a meal slot's default option to create, with its portions and
// the slot's other options.
type NewMeal struct {
	Name        string
	OptionLabel string
	Portions    []NewPortion
	// Alternatives are the slot's further options, in order. Their own
	// Alternatives are ignored.
	Alternatives []NewMeal
}

// NewPortion is an ingredient at a quantity, with the macros it adds already
// worked out, so the repository never needs the ingredient's profile.
type NewPortion struct {
	IngredientID  uuid.UUID
	QuantityGrams float64
	Macros        Macros
	SourceText    string
	Estimated     bool
}

// CreatePlan writes a plan, its days and any meals, options and portions they
// come with, all or nothing. The plan's total is summed once at the end: an
// imported plan with every day filled is a couple of hundred meals.
func (r *Repository) CreatePlan(ctx context.Context, userID uuid.UUID, plan NewPlan, days []NewDay) (uuid.UUID, error) {
	var planID uuid.UUID
	err := r.inTx(ctx, func(q *mealsdb.Queries) error {
		row, err := q.CreateMealPlan(ctx, mealsdb.CreateMealPlanParams{
			UserID: userID, Name: plan.Name, Description: plan.Description, Objective: plan.Objective,
			ActivityLevel: plan.ActivityLevel, Gender: plan.Gender,
			PlanType: string(plan.Settings.Type), CustomCarbPct: plan.Settings.CustomCarbPct, Mode: string(plan.Settings.Mode),
			Notes: plan.Notes,
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
				if err := tx.createSlot(ctx, planID, day.ID, m); err != nil {
					return err
				}
			}
		}
		return apperr.Wrap(recalculatePlanTotals(ctx, q, planID), "recalculate plan totals")
	})
	return planID, err
}

// createSlot writes a meal slot's default and its alternatives with their
// portions and each meal's own total, leaving the plan's total to the caller.
func (tx *PlanTx) createSlot(ctx context.Context, planID, dayID uuid.UUID, m NewMeal) error {
	created, err := tx.AddMeal(ctx, planID, dayID, m.Name, m.OptionLabel)
	if err != nil {
		return err
	}
	if err = tx.insertPortions(ctx, created.ID, m.Portions); err != nil {
		return err
	}
	for _, alt := range m.Alternatives {
		option, err := tx.AddOption(ctx, planID, dayID, created.MealNumber, alt.Name, alt.OptionLabel)
		if err != nil {
			return err
		}
		if err = tx.insertPortions(ctx, option.ID, alt.Portions); err != nil {
			return err
		}
	}
	return nil
}

// insertPortions adds portions to a meal and recalculates that meal's total,
// not the plan's.
func (tx *PlanTx) insertPortions(ctx context.Context, mealID uuid.UUID, portions []NewPortion) error {
	if len(portions) == 0 {
		return nil
	}
	for _, p := range portions {
		if _, err := tx.createPortion(ctx, mealID, p); err != nil {
			return err
		}
	}
	return apperr.Wrap(recalculateMealTotal(ctx, tx.q, mealID), "recalculate meal total")
}

func (tx *PlanTx) createPortion(ctx context.Context, mealID uuid.UUID, p NewPortion) (MealIngredient, error) {
	row, err := tx.q.CreateMealIngredient(ctx, mealsdb.CreateMealIngredientParams{
		MealID: mealID, IngredientID: p.IngredientID, QuantityGrams: p.QuantityGrams,
		Calories: p.Macros.Calories, ProteinG: p.Macros.ProteinG, FatG: p.Macros.FatG, CarbsG: p.Macros.CarbG,
		SourceText: p.SourceText, Estimated: p.Estimated,
	})
	if err != nil {
		return MealIngredient{}, apperr.Wrap(err, "create meal ingredient")
	}
	return mealIngredientFromDB(row), nil
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

// AddMeal adds a meal slot at the end of a day, as the slot's default option.
func (tx *PlanTx) AddMeal(ctx context.Context, planID, dayID uuid.UUID, name, optionLabel string) (Meal, error) {
	row, err := tx.q.CreateMeal(ctx, mealsdb.CreateMealParams{MealPlanID: planID, DayID: dayID, Name: name, OptionLabel: optionLabel})
	if err != nil {
		return Meal{}, apperr.Wrap(err, "create meal")
	}
	return mealFromDB(row), nil
}

// AddOption adds an empty option to a day's meal slot, after its last one.
func (tx *PlanTx) AddOption(ctx context.Context, planID, dayID uuid.UUID, mealNumber int, name, optionLabel string) (Meal, error) {
	row, err := tx.q.CreateMealOption(ctx, mealsdb.CreateMealOptionParams{
		MealPlanID: planID, DayID: dayID, Name: name, MealNumber: int16(mealNumber), OptionLabel: optionLabel,
	})
	if err != nil {
		return Meal{}, apperr.Wrap(err, "create meal option")
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
		row, err := tx.createPortion(ctx, mealID, p)
		if err != nil {
			return nil, err
		}
		added = append(added, row)
	}
	if err := recalculateTotals(ctx, tx.q, mealID, planID); err != nil {
		return nil, apperr.Wrap(err, "recalculate totals after adding ingredients")
	}
	return added, nil
}

// Plan reads the plan back inside the transaction, seeing the changes made
// so far; with its meals' ingredients when withIngredients is set.
// apperr.ErrNotFound if the plan is not the user's.
func (tx *PlanTx) Plan(ctx context.Context, planID, userID uuid.UUID, withIngredients bool) (MealPlan, error) {
	row, err := tx.q.GetMealPlan(ctx, mealsdb.GetMealPlanParams{ID: planID, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MealPlan{}, apperr.ErrNotFound
		}
		return MealPlan{}, apperr.Wrap(err, "get meal plan")
	}
	return loadPlan(ctx, tx.q, row, withIngredients)
}

// RemoveMeal deletes a meal of the plan with its ingredients and recalculates
// the plan's totals. A slot's default takes the whole slot with it, every
// option; an alternative goes alone.
func (tx *PlanTx) RemoveMeal(ctx context.Context, planID uuid.UUID, m Meal) error {
	var err error
	if m.OptionIndex == 1 {
		err = tx.q.DeleteMealSlot(ctx, mealsdb.DeleteMealSlotParams{MealPlanID: planID, DayID: m.DayID, MealNumber: int16(m.MealNumber)})
	} else {
		err = tx.q.DeleteMealOfPlan(ctx, mealsdb.DeleteMealOfPlanParams{ID: m.ID, MealPlanID: planID})
	}
	if err != nil {
		return apperr.Wrap(err, "delete meal")
	}
	return apperr.Wrap(recalculatePlanTotals(ctx, tx.q, planID), "recalculate plan totals after removing meal")
}

// RemovePortion deletes one ingredient of a meal and recalculates the meal's
// and the plan's totals.
func (tx *PlanTx) RemovePortion(ctx context.Context, planID, mealID, portionID uuid.UUID) error {
	if err := tx.q.DeleteMealIngredient(ctx, mealsdb.DeleteMealIngredientParams{ID: portionID, MealID: mealID}); err != nil {
		return apperr.Wrap(err, "delete meal ingredient")
	}
	return apperr.Wrap(recalculateTotals(ctx, tx.q, mealID, planID), "recalculate totals after removing ingredient")
}

// SetPortionGrams changes how much of an ingredient a meal has, storing the
// macros the caller worked out for the new quantity, and recalculates the
// meal's and the plan's totals. apperr.ErrNotFound if the portion is not in
// the meal.
func (tx *PlanTx) SetPortionGrams(ctx context.Context, planID, mealID, portionID uuid.UUID, grams float64, macros Macros) (MealIngredient, error) {
	row, err := tx.q.UpdateMealIngredientQuantity(ctx, mealsdb.UpdateMealIngredientQuantityParams{
		ID: portionID, MealID: mealID, QuantityGrams: grams,
		Calories: macros.Calories, ProteinG: macros.ProteinG, FatG: macros.FatG, CarbsG: macros.CarbG,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MealIngredient{}, apperr.ErrNotFound
		}
		return MealIngredient{}, apperr.Wrap(err, "update meal ingredient quantity")
	}
	if err := recalculateTotals(ctx, tx.q, mealID, planID); err != nil {
		return MealIngredient{}, apperr.Wrap(err, "recalculate totals after changing a quantity")
	}
	return mealIngredientFromDB(row), nil
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
	planID, _, err := r.LocateMealIngredient(ctx, mealIngredientID, userID)
	return planID, err
}

// LocateMealIngredient resolves the plan and meal a meal's ingredient belongs
// to. apperr.ErrNotFound if it is not the user's.
func (r *Repository) LocateMealIngredient(ctx context.Context, mealIngredientID, userID uuid.UUID) (planID, mealID uuid.UUID, err error) {
	owned, err := r.q.GetMealIngredientOwned(ctx, mealsdb.GetMealIngredientOwnedParams{ID: mealIngredientID, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, uuid.Nil, apperr.ErrNotFound
		}
		return uuid.Nil, uuid.Nil, apperr.Wrap(err, "get meal ingredient")
	}
	planID, err = r.PlanIDOfMeal(ctx, owned.OwnedMealID, userID)
	return planID, owned.OwnedMealID, err
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
// meal's ingredients when withIngredients is set. A day's Meals are its slots'
// defaults, each holding the slot's other options as Alternatives.
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
		day := &plan.Days[byID[m.DayID]]
		// Rows come in slot then option order, so a slot's default is already
		// in place. An alternative whose default is missing stands in for it.
		if m.OptionIndex > 1 {
			if k := slices.IndexFunc(day.Meals, func(d Meal) bool { return d.MealNumber == m.MealNumber }); k >= 0 {
				day.Meals[k].Alternatives = append(day.Meals[k].Alternatives, m)
				continue
			}
		}
		day.Meals = append(day.Meals, m)
	}
	return plan, nil
}

// recalculateTotals sums a meal's ingredients into its total_macros, then
// sums the plan's meals into the plan's total_macros. Ports FitMe's
// calculateTotals two levels deep; callers run it in their transaction so a
// reader never sees a meal and plan total momentarily out of sync.
func recalculateTotals(ctx context.Context, q *mealsdb.Queries, mealID, planID uuid.UUID) error {
	if err := recalculateMealTotal(ctx, q, mealID); err != nil {
		return err
	}
	return recalculatePlanTotals(ctx, q, planID)
}

// recalculateMealTotal sums one meal's ingredients into its total_macros. Every
// option has its own, for showing and logging it.
func recalculateMealTotal(ctx context.Context, q *mealsdb.Queries, mealID uuid.UUID) error {
	sums, err := q.SumMealIngredientMacros(ctx, mealID)
	if err != nil {
		return apperr.Wrap(err, "sum meal ingredients")
	}
	mealMacros := Macros{Calories: sums.Calories, ProteinG: sums.ProteinG, FatG: sums.FatG, CarbG: sums.CarbsG}
	return apperr.Wrap(q.UpdateMealTotalMacros(ctx, mealsdb.UpdateMealTotalMacrosParams{ID: mealID, TotalMacros: macrosToJSON(mealMacros)}),
		"update meal total macros")
}

// recalculatePlanTotals re-sums a plan's meals without touching any single
// meal's own total — used after a meal or day is removed, where there is no
// meal left to recompute. Only each slot's default counts, the same meals
// loadPlan puts in a day's Meals: alternatives replace their default, they are
// not eaten on top of it.
func recalculatePlanTotals(ctx context.Context, q *mealsdb.Queries, planID uuid.UUID) error {
	siblings, err := q.ListMealsByPlan(ctx, planID)
	if err != nil {
		return apperr.Wrap(err, "list meals by plan")
	}
	type slot struct {
		day    uuid.UUID
		number int16
	}
	counted := map[slot]bool{}
	var planTotal Macros
	for _, sibling := range siblings {
		key := slot{sibling.DayID, sibling.MealNumber}
		if counted[key] {
			continue
		}
		counted[key] = true
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
		Notes:       row.Notes,
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
		OptionIndex: int(row.OptionIndex), OptionLabel: row.OptionLabel,
		TotalMacros: macrosFromJSON(row.TotalMacros),
		CreatedAt:   row.CreatedAt,
	}
}

func mealIngredientFromDB(row mealsdb.MealIngredient) MealIngredient {
	return MealIngredient{
		ID: row.ID, MealID: row.MealID, IngredientID: row.IngredientID,
		QuantityGrams: row.QuantityGrams,
		Macros:        Macros{Calories: row.Calories, ProteinG: row.ProteinG, FatG: row.FatG, CarbG: row.CarbsG},
		SourceText:    row.SourceText,
		Estimated:     row.Estimated,
		CreatedAt:     row.CreatedAt,
	}
}

func mealIngredientFromListRow(row mealsdb.ListMealIngredientsRow) MealIngredient {
	return MealIngredient{
		ID: row.ID, MealID: row.MealID, IngredientID: row.IngredientID,
		IngredientName: row.IngredientName,
		QuantityGrams:  row.QuantityGrams,
		Macros:         Macros{Calories: row.Calories, ProteinG: row.ProteinG, FatG: row.FatG, CarbG: row.CarbsG},
		SourceText:     row.SourceText,
		Estimated:      row.Estimated,
		CreatedAt:      row.CreatedAt,
	}
}
