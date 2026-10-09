package meals

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/meals/meal"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Editing a plan as a list of changes.
//
// "Add 30 g oats to breakfast every day, and drop Tuesday's snack" is one
// request, and the coach shows the person one approval card per call — so it
// arrives as one list, applied in one transaction under the plan's lock and
// checked against the target once, on the result. Either every change lands
// or none does: a plan with half an edit is one the person did not ask for.
//
// Meals and foods are named, not identified, because they are named in
// conversation; the names are resolved here, against the plan as it stands
// part-way through the list, so a meal added by one change can be filled by
// the next.

// PlanChangeOp is one kind of change ApplyChanges makes.
type PlanChangeOp string

const (
	// OpAddMeal adds an empty meal at the end of each day that lacks it.
	OpAddMeal PlanChangeOp = "add_meal"
	// OpRemoveMeal deletes the meal with its foods.
	OpRemoveMeal PlanChangeOp = "remove_meal"
	// OpAddFood adds a portion of IngredientID to the meal.
	OpAddFood PlanChangeOp = "add_food"
	// OpRemoveFood takes every portion of the food off the meal.
	OpRemoveFood PlanChangeOp = "remove_food"
	// OpSetGrams makes the meal hold Grams of the food, in one portion.
	OpSetGrams PlanChangeOp = "set_grams"
)

// PlanChangeOps lists every op, in the order a tool schema offers them.
var PlanChangeOps = []PlanChangeOp{OpAddMeal, OpRemoveMeal, OpAddFood, OpRemoveFood, OpSetGrams}

func (o PlanChangeOp) Valid() bool { return slices.Contains(PlanChangeOps, o) }

// usesFood reports whether the op names a food within the meal.
func (o PlanChangeOp) usesFood() bool {
	return o == OpAddFood || o == OpRemoveFood || o == OpSetGrams
}

// PlanChange is one edit to a plan's meals.
type PlanChange struct {
	Op PlanChangeOp
	// Days are the weekdays to change; empty is every day of the plan. A
	// change applies on the days where what it names exists, and is refused
	// only when it matches on none of them.
	Days []time.Weekday
	// Meal is the meal's name: matched case-insensitively within each day,
	// exactly first and then as a unique part of a name. add_meal creates it.
	Meal string
	// IngredientID is the food add_food adds. remove_food and set_grams find
	// their food by it too when it is set, and by Food otherwise.
	IngredientID uuid.UUID
	// Food finds the food already in the meal by its name, the way Meal finds
	// the meal, for remove_food and set_grams.
	Food string
	// Grams is how much add_food adds, or how much set_grams leaves.
	Grams float64
}

// AppliedChange is what one change did.
type AppliedChange struct {
	Change PlanChange
	// Meal and Food are the names as the plan has them.
	Meal string
	Food string
	// Weekdays are the days the change was made on, in plan order.
	Weekdays []time.Weekday
}

// ApplyChanges makes every change to the plan, in order, in one transaction,
// and checks the result against the person's target once: a plan the changes
// take further over is refused with an *OverageError unless confirm is set
// (see CheckWrite). Nothing is written unless everything is.
func (s *MealPlanService) ApplyChanges(ctx context.Context, planID, userID uuid.UUID, changes []PlanChange, confirm bool) (MealPlan, []AppliedChange, error) {
	changes, err := validatePlanChanges(changes)
	if err != nil {
		return MealPlan{}, nil, err
	}
	foods, err := s.changeIngredients(ctx, userID, changes)
	if err != nil {
		return MealPlan{}, nil, err
	}
	active, err := s.requireTarget(ctx, userID)
	if err != nil {
		return MealPlan{}, nil, err
	}

	var applied []AppliedChange
	err = s.repo.WithPlanLocked(ctx, planID, userID, func(tx *PlanTx, plan MealPlan) error {
		// The lock-time plan has no ingredients, and is the "before" the
		// overage rule compares against; this copy is edited as we go.
		working, err := tx.Plan(ctx, planID, userID, true)
		if err != nil {
			return err
		}
		ed := planEditor{tx: tx, plan: &working, foods: foods}
		for i, c := range changes {
			done, err := ed.apply(ctx, c)
			if err != nil {
				return apperr.Wrap(err, "change %d (%s)", i+1, c.Op)
			}
			applied = append(applied, done)
		}

		after, err := tx.Plan(ctx, planID, userID, false)
		if err != nil {
			return err
		}
		return checkOverage(active, plan, after.State(), false, confirm)
	})
	if err != nil {
		return MealPlan{}, nil, err
	}

	stored, err := s.repo.GetPlan(ctx, planID, userID)
	return stored, applied, err
}

// validatePlanChanges checks each change on its own, before anything is
// looked up.
func validatePlanChanges(changes []PlanChange) ([]PlanChange, error) {
	if len(changes) == 0 {
		return nil, apperr.Wrap(apperr.ErrValidation, "name at least one change")
	}
	out := make([]PlanChange, len(changes))
	for i, c := range changes {
		c.Meal = strings.TrimSpace(c.Meal)
		c.Food = strings.TrimSpace(c.Food)
		if err := validatePlanChange(c); err != nil {
			return nil, apperr.Wrap(err, "change %d (%s)", i+1, c.Op)
		}
		out[i] = c
	}
	return out, nil
}

func validatePlanChange(c PlanChange) error {
	invalid := func(format string, args ...any) error {
		return apperr.Wrap(apperr.ErrValidation, format, args...)
	}
	if !c.Op.Valid() {
		return invalid("unknown change %q", c.Op)
	}
	if c.Meal == "" {
		return invalid("name the meal")
	}
	seen := map[time.Weekday]bool{}
	for _, wd := range c.Days {
		if wd < time.Sunday || wd > time.Saturday || seen[wd] {
			return invalid("name each weekday at most once")
		}
		seen[wd] = true
	}
	switch c.Op {
	case OpAddFood:
		if c.IngredientID == uuid.Nil {
			return invalid("name the food to add")
		}
	case OpRemoveFood, OpSetGrams:
		if c.IngredientID == uuid.Nil && c.Food == "" {
			return invalid("name the food to change")
		}
	}
	if (c.Op == OpAddFood || c.Op == OpSetGrams) && c.Grams <= 0 {
		return invalid("give an amount greater than zero grams")
	}
	return nil
}

// changeIngredients loads every food add_food will add, so one this account
// cannot see refuses the batch before the plan is locked.
func (s *MealPlanService) changeIngredients(ctx context.Context, userID uuid.UUID, changes []PlanChange) (map[uuid.UUID]Ingredient, error) {
	out := map[uuid.UUID]Ingredient{}
	for _, c := range changes {
		if c.Op != OpAddFood {
			continue
		}
		if _, ok := out[c.IngredientID]; ok {
			continue
		}
		ingredient, err := s.repo.GetIngredient(ctx, c.IngredientID, userID)
		if err != nil {
			return nil, err
		}
		out[c.IngredientID] = ingredient
	}
	return out, nil
}

// planEditor applies changes through tx and keeps plan — the working copy,
// ingredients included — in step with what it wrote, so later changes see
// earlier ones.
type planEditor struct {
	tx    *PlanTx
	plan  *MealPlan
	foods map[uuid.UUID]Ingredient
}

// dayResult is what one change did, or did not find, on one day.
type dayResult struct {
	changed  bool
	meal     string
	food     string
	noFood   bool     // the meal is there, without the food
	hasMeals []string // the day's meals, when the named one is not among them
}

func (ed planEditor) apply(ctx context.Context, c PlanChange) (AppliedChange, error) {
	days, err := targetDays(*ed.plan, c.Days)
	if err != nil {
		return AppliedChange{}, err
	}

	done := AppliedChange{Change: c}
	var missedFood bool
	mealNames := map[string]bool{}
	for _, i := range days {
		res, err := ed.applyToDay(ctx, &ed.plan.Days[i], c)
		if err != nil {
			return AppliedChange{}, err
		}
		if res.changed {
			done.Weekdays = append(done.Weekdays, ed.plan.Days[i].Weekday)
			done.Meal, done.Food = res.meal, res.food
		}
		missedFood = missedFood || res.noFood
		for _, name := range res.hasMeals {
			mealNames[name] = true
		}
	}
	if len(done.Weekdays) > 0 {
		return done, nil
	}
	return AppliedChange{}, nothingMatched(c, *ed.plan, days, missedFood, mealNames)
}

// nothingMatched names what a change could not find on any of its days.
func nothingMatched(c PlanChange, plan MealPlan, days []int, missedFood bool, mealNames map[string]bool) error {
	where := dayNames(plan, days)
	switch {
	case c.Op == OpAddMeal:
		return apperr.Wrap(apperr.ErrValidation, "%s already has a meal called %q", where, c.Meal)
	case missedFood:
		food := c.Food
		if ing, ok := firstIngredientName(plan, c.IngredientID); ok && food == "" {
			food = ing
		}
		food = strconv.Quote(food)
		if food == `""` {
			food = "that food"
		}
		return apperr.Wrap(apperr.ErrValidation, "%s is not in %s on %s", food, c.Meal, where)
	default:
		names := make([]string, 0, len(mealNames))
		for name := range mealNames {
			names = append(names, strconv.Quote(name))
		}
		slices.Sort(names)
		if len(names) == 0 {
			return apperr.Wrap(apperr.ErrValidation, "there is no meal called %q on %s, which has no meals yet", c.Meal, where)
		}
		return apperr.Wrap(apperr.ErrValidation, "there is no meal called %q on %s; the meals there are %s",
			c.Meal, where, strings.Join(names, ", "))
	}
}

func (ed planEditor) applyToDay(ctx context.Context, day *meal.Day, c PlanChange) (dayResult, error) {
	planID := ed.plan.ID

	if c.Op == OpAddMeal {
		if _, exists := exactMeal(*day, c.Meal); exists {
			return dayResult{}, nil
		}
		added, err := ed.tx.AddMeal(ctx, planID, day.ID, c.Meal)
		if err != nil {
			return dayResult{}, err
		}
		day.Meals = append(day.Meals, added)
		return dayResult{changed: true, meal: added.Name}, nil
	}

	mi, found, err := PickMeal(*day, c.Meal)
	if err != nil {
		return dayResult{}, err
	}
	if !found {
		return dayResult{hasMeals: mealNamesOf(*day)}, nil
	}
	m := &day.Meals[mi]

	switch c.Op {
	case OpRemoveMeal:
		if err := ed.tx.RemoveMeal(ctx, planID, m.ID); err != nil {
			return dayResult{}, err
		}
		name := m.Name
		day.Meals = slices.Delete(day.Meals, mi, mi+1)
		return dayResult{changed: true, meal: name}, nil

	case OpAddFood:
		ingredient := ed.foods[c.IngredientID]
		added, err := ed.tx.AddPortions(ctx, planID, m.ID, []NewPortion{{
			IngredientID: ingredient.ID, QuantityGrams: c.Grams, Macros: ingredient.MacrosFor(c.Grams),
		}})
		if err != nil {
			return dayResult{}, err
		}
		added[0].IngredientName = ingredient.Name
		m.Ingredients = append(m.Ingredients, added[0])
		return dayResult{changed: true, meal: m.Name, food: ingredient.Name}, nil
	}

	portions, err := pickPortions(*m, c.IngredientID, c.Food)
	if err != nil {
		return dayResult{}, apperr.Wrap(err, "%s on %s", m.Name, day.Weekday)
	}
	if len(portions) == 0 {
		return dayResult{noFood: true}, nil
	}
	food := m.Ingredients[portions[0]].IngredientName

	if c.Op == OpSetGrams {
		// One portion keeps the food, at the new weight, with its snapshot
		// scaled rather than re-read from the catalog: a meal's macros are
		// what the food was when it was planned. Any further portions of the
		// same food go, so the meal ends up holding exactly Grams of it.
		first := &m.Ingredients[portions[0]]
		updated, err := ed.tx.SetPortionGrams(ctx, planID, m.ID, first.ID, c.Grams,
			scaleMacros(first.Macros, c.Grams/first.QuantityGrams))
		if err != nil {
			return dayResult{}, err
		}
		updated.IngredientName = first.IngredientName
		*first = updated
		portions = portions[1:]
	}
	// Back to front, so the indexes still point at the right portions.
	for k := len(portions) - 1; k >= 0; k-- {
		p := portions[k]
		if err := ed.tx.RemovePortion(ctx, planID, m.ID, m.Ingredients[p].ID); err != nil {
			return dayResult{}, err
		}
		m.Ingredients = slices.Delete(m.Ingredients, p, p+1)
	}
	return dayResult{changed: true, meal: m.Name, food: food}, nil
}

// targetDays resolves weekdays to positions in plan.Days; none is every day.
// A weekday the plan does not have is an error naming the days it does.
func targetDays(plan MealPlan, weekdays []time.Weekday) ([]int, error) {
	if len(weekdays) == 0 {
		out := make([]int, len(plan.Days))
		for i := range plan.Days {
			out[i] = i
		}
		return out, nil
	}
	out := make([]int, 0, len(weekdays))
	for _, wd := range weekdays {
		i := slices.IndexFunc(plan.Days, func(d meal.Day) bool { return d.Weekday == wd })
		if i < 0 {
			return nil, apperr.Wrap(apperr.ErrValidation, "the plan has no %s; its days are %s",
				wd, strings.Join(weekdayNames(plan.Weekdays()), ", "))
		}
		out = append(out, i)
	}
	return out, nil
}

// PickMeal finds the meal a name refers to within a day: an exact match,
// ignoring case, or else the one meal whose name contains it. found is false
// when nothing matches; several matches is an error listing them, because
// guessing would edit the wrong meal silently.
func PickMeal(day meal.Day, name string) (index int, found bool, err error) {
	if i, ok := exactMeal(day, name); ok {
		return i, true, nil
	}
	needle := strings.ToLower(strings.TrimSpace(name))
	var hits []int
	for i, m := range day.Meals {
		if strings.Contains(strings.ToLower(m.Name), needle) {
			hits = append(hits, i)
		}
	}
	switch len(hits) {
	case 0:
		return 0, false, nil
	case 1:
		return hits[0], true, nil
	default:
		matched := make([]string, len(hits))
		for k, i := range hits {
			matched[k] = strconv.Quote(day.Meals[i].Name)
		}
		return 0, false, apperr.Wrap(apperr.ErrValidation, "%q matches several meals on %s (%s); say which",
			name, day.Weekday, strings.Join(matched, ", "))
	}
}

// exactMeal finds a meal named exactly name, ignoring case. Two meals of one
// day sharing a name resolve to the first.
func exactMeal(day meal.Day, name string) (int, bool) {
	needle := strings.TrimSpace(name)
	for i, m := range day.Meals {
		if strings.EqualFold(m.Name, needle) {
			return i, true
		}
	}
	return 0, false
}

// pickPortions finds every portion of one food in a meal: by ingredient when
// id is set, otherwise by name the way PickMeal finds a meal. None is not an
// error; a name matching two different foods is.
func pickPortions(m meal.Meal, id uuid.UUID, food string) ([]int, error) {
	if id == uuid.Nil {
		var err error
		if id, err = foodInMeal(m, food); err != nil || id == uuid.Nil {
			return nil, err
		}
	}
	var out []int
	for i, p := range m.Ingredients {
		if p.IngredientID == id {
			out = append(out, i)
		}
	}
	return out, nil
}

// foodInMeal resolves a food name to the ingredient it names in a meal;
// uuid.Nil when nothing in the meal matches.
func foodInMeal(m meal.Meal, food string) (uuid.UUID, error) {
	needle := strings.ToLower(strings.TrimSpace(food))
	for _, p := range m.Ingredients {
		if strings.ToLower(p.IngredientName) == needle {
			return p.IngredientID, nil
		}
	}
	var ids []uuid.UUID
	var matched []string
	for _, p := range m.Ingredients {
		if strings.Contains(strings.ToLower(p.IngredientName), needle) && !slices.Contains(ids, p.IngredientID) {
			ids = append(ids, p.IngredientID)
			matched = append(matched, strconv.Quote(p.IngredientName))
		}
	}
	switch len(ids) {
	case 0:
		return uuid.Nil, nil
	case 1:
		return ids[0], nil
	default:
		return uuid.Nil, apperr.Wrap(apperr.ErrValidation, "%q could mean %s; say which", food, strings.Join(matched, " or "))
	}
}

func firstIngredientName(plan MealPlan, id uuid.UUID) (string, bool) {
	if id == uuid.Nil {
		return "", false
	}
	for _, d := range plan.Days {
		for _, m := range d.Meals {
			for _, p := range m.Ingredients {
				if p.IngredientID == id {
					return p.IngredientName, true
				}
			}
		}
	}
	return "", false
}

func mealNamesOf(day meal.Day) []string {
	out := make([]string, len(day.Meals))
	for i, m := range day.Meals {
		out[i] = m.Name
	}
	return out
}

// dayNames lists the plan's days at positions, as "every day" when they are
// all of a plan's several days.
func dayNames(plan MealPlan, positions []int) string {
	if len(positions) == len(plan.Days) && len(plan.Days) > 1 {
		return "every day (" + strings.Join(weekdayNames(plan.Weekdays()), ", ") + ")"
	}
	days := make([]time.Weekday, len(positions))
	for k, i := range positions {
		days[k] = plan.Days[i].Weekday
	}
	return strings.Join(weekdayNames(days), ", ")
}

func weekdayNames(days []time.Weekday) []string {
	out := make([]string, len(days))
	for i, wd := range days {
		out[i] = wd.String()
	}
	return out
}

func scaleMacros(m Macros, factor float64) Macros {
	return Macros{
		Calories: m.Calories * factor,
		ProteinG: m.ProteinG * factor,
		FatG:     m.FatG * factor,
		CarbG:    m.CarbG * factor,
	}
}

// String renders a change the way a summary names it, such as
// "add 30 g Oats to Breakfast".
func (a AppliedChange) String() string {
	var what string
	switch a.Change.Op {
	case OpAddMeal:
		what = fmt.Sprintf("added the meal %s", a.Meal)
	case OpRemoveMeal:
		what = fmt.Sprintf("removed %s", a.Meal)
	case OpAddFood:
		what = fmt.Sprintf("added %.0f g %s to %s", a.Change.Grams, a.Food, a.Meal)
	case OpRemoveFood:
		what = fmt.Sprintf("removed %s from %s", a.Food, a.Meal)
	case OpSetGrams:
		what = fmt.Sprintf("set %s in %s to %.0f g", a.Food, a.Meal, a.Change.Grams)
	}
	return what + " on " + strings.Join(weekdayNames(a.Weekdays), ", ")
}
