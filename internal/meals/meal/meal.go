// Package meal holds the shapes for North's nutrition domain: ingredients,
// diet preferences, meal plans, food log entries, and meal reminders.
//
// A leaf, so the meals service and anything that renders one of these do not
// import each other. See CLAUDE.md on slice layout.
package meal

import (
	"time"

	"github.com/google/uuid"
)

const (
	kcalPerGramProteinOrCarb = 4
	kcalPerGramFat           = 9
)

// Ingredient categories. Broad on purpose, same reasoning as goal
// categories: a fine-grained taxonomy makes people stop and classify instead
// of log their food.
const (
	CategoryProtein   = "protein"
	CategoryCarb      = "carb"
	CategoryFat       = "fat"
	CategoryDairy     = "dairy"
	CategoryVegetable = "vegetable"
	CategoryFruit     = "fruit"
	CategoryBeverage  = "beverage"
	CategorySnack     = "snack"
	CategoryOther     = "other"
)

// Categories is the ordered set offered in the UI.
var Categories = []string{
	CategoryProtein, CategoryCarb, CategoryFat, CategoryDairy,
	CategoryVegetable, CategoryFruit, CategoryBeverage, CategorySnack, CategoryOther,
}

// Macros is a nutrient total: calories plus the three macronutrients in
// grams. Fiber/sugar/sodium/potassium/cholesterol live only on Ingredient,
// where they are informational; they are not carried through meals and logs.
type Macros struct {
	Calories float64
	ProteinG float64
	FatG     float64
	CarbG    float64
}

// Add combines two macro totals, e.g. summing a meal's ingredients.
func (m Macros) Add(o Macros) Macros {
	return Macros{
		Calories: m.Calories + o.Calories,
		ProteinG: m.ProteinG + o.ProteinG,
		FatG:     m.FatG + o.FatG,
		CarbG:    m.CarbG + o.CarbG,
	}
}

// MacrosFromGrams derives calories from grams of each macronutrient, for
// ingredients that only store the per-100g breakdown.
func MacrosFromGrams(proteinG, fatG, carbG float64) Macros {
	return Macros{
		Calories: proteinG*kcalPerGramProteinOrCarb + fatG*kcalPerGramFat + carbG*kcalPerGramProteinOrCarb,
		ProteinG: proteinG,
		FatG:     fatG,
		CarbG:    carbG,
	}
}

// Ingredient is a food item's nutrient profile, stored per 100g so any
// logged quantity scales cleanly.
type Ingredient struct {
	ID uuid.UUID

	// UserID is nil for a shared/global ingredient anyone can log against,
	// and set for one a user created themselves.
	UserID *uuid.UUID

	Name     string
	Brand    string
	Category string

	// ServingSizeGrams is display-only ("1 medium egg = 50g"); MacrosFor
	// always computes from Per100g, never from a serving count.
	ServingSizeGrams float64
	Per100g          Macros

	// SaturatedFatGPer100g is tracked separately from total fat: it is the
	// one fat number a coach has anything specific to say about.
	SaturatedFatGPer100g float64

	FiberGPer100g        float64
	SugarGPer100g        float64
	SodiumMgPer100g      float64
	PotassiumMgPer100g   float64
	CholesterolMgPer100g float64

	CreatedAt time.Time
	UpdatedAt time.Time
}

// MacrosFor scales the per-100g profile to an actual logged quantity.
func (i Ingredient) MacrosFor(quantityGrams float64) Macros {
	factor := quantityGrams / 100
	return Macros{
		Calories: i.Per100g.Calories * factor,
		ProteinG: i.Per100g.ProteinG * factor,
		FatG:     i.Per100g.FatG * factor,
		CarbG:    i.Per100g.CarbG * factor,
	}
}

// Diet is a reference diet type (vegan, keto, ...), seeded once and never
// user-editable.
type Diet struct {
	ID          uuid.UUID
	Code        string
	Name        string
	Description string
}

// MealPlan groups a week's days of meals around a stated objective, each day
// held to a target taken from the person's current macro plan (see
// plan_rules.go).
type MealPlan struct {
	ID     uuid.UUID
	UserID uuid.UUID

	Name          string
	Description   string
	Objective     string
	ActivityLevel string
	Gender        string

	Settings PlanSettings

	// Notes is free text an imported plan carried beside its meals.
	Notes string

	// TotalMacros is a cache kept current by the service on every ingredient
	// add/remove, not re-summed on every read. It spans every day, counting
	// each slot's default option only.
	TotalMacros Macros
	// Days run Monday first. ListPlans loads them without their meals.
	Days []Day

	CreatedAt time.Time
	UpdatedAt time.Time
}

// State is the plan as the overage rule sees it.
func (p MealPlan) State() PlanState {
	days := make([]DayState, len(p.Days))
	for i, d := range p.Days {
		days[i] = DayState{ID: d.ID, Weekday: d.Weekday, Override: d.Override, Consumed: d.Consumed()}
	}
	return PlanState{Settings: p.Settings, Days: days}
}

// DayIndex finds a day of the plan by id.
func (p MealPlan) DayIndex(dayID uuid.UUID) (int, bool) {
	for i, d := range p.Days {
		if d.ID == dayID {
			return i, true
		}
	}
	return 0, false
}

// DayIndexOfMeal finds the day of the plan holding a meal, default or
// alternative.
func (p MealPlan) DayIndexOfMeal(mealID uuid.UUID) (int, bool) {
	day, _, ok := p.LocateMeal(mealID)
	return day, ok
}

// LocateMeal finds the day of the plan holding a meal, and whether the meal is
// an alternative option of its slot rather than the default.
func (p MealPlan) LocateMeal(mealID uuid.UUID) (day int, alternative bool, ok bool) {
	for i, d := range p.Days {
		for _, m := range d.Meals {
			if m.ID == mealID {
				return i, false, true
			}
			for _, alt := range m.Alternatives {
				if alt.ID == mealID {
					return i, true, true
				}
			}
		}
	}
	return 0, false, false
}

// Weekdays lists the weekdays the plan covers.
func (p MealPlan) Weekdays() []time.Weekday {
	out := make([]time.Weekday, len(p.Days))
	for i, d := range p.Days {
		out[i] = d.Weekday
	}
	return out
}

// Day is one weekday of a plan and its meals.
type Day struct {
	ID       uuid.UUID
	PlanID   uuid.UUID
	Weekday  time.Weekday
	Override DayOverride
	// Meals holds each slot's default option; the others hang off it as
	// Alternatives.
	Meals []Meal
}

// Consumed is what the day's meals add up to: each slot's default option.
// Alternatives are interchangeable with their default, not eaten on top of
// it, so they are not counted.
func (d Day) Consumed() Macros {
	var total Macros
	for _, m := range d.Meals {
		total = total.Add(m.TotalMacros)
	}
	return total
}

// Meal is one meal within a day of a plan (breakfast, lunch, ...), ordered by
// MealNumber within its day.
//
// A meal slot can hold several interchangeable options, each a Meal sharing
// the slot's MealNumber. Option 1 is the default, the one a day's totals
// count; the rest are its Alternatives, ordered by OptionIndex, which may have
// gaps.
type Meal struct {
	ID         uuid.UUID
	MealPlanID uuid.UUID
	DayID      uuid.UUID

	MealNumber  int
	OptionIndex int
	// OptionLabel tells an option apart from its slot's others ("Opção 2");
	// empty for a slot's only option.
	OptionLabel string
	Name        string

	TotalMacros Macros
	Ingredients []MealIngredient

	// Alternatives are set on a default only, never on an alternative.
	Alternatives []Meal

	CreatedAt time.Time
}

// Options lists the slot's options: the default, then its alternatives.
func (m Meal) Options() []Meal {
	out := make([]Meal, 0, 1+len(m.Alternatives))
	out = append(out, m)
	return append(out, m.Alternatives...)
}

// DisplayName is the meal's name with its option label, as a log entry or a
// picker names it.
func (m Meal) DisplayName() string {
	if m.OptionLabel == "" {
		return m.Name
	}
	return m.Name + " · " + m.OptionLabel
}

// MealIngredient is one ingredient within a meal, at a specific quantity.
// Macros is a snapshot taken at insert time, so editing the underlying
// ingredient later never silently rewrites a meal's history.
type MealIngredient struct {
	ID           uuid.UUID
	MealID       uuid.UUID
	IngredientID uuid.UUID

	// IngredientName is denormalized for display without an extra join.
	IngredientName string

	QuantityGrams float64
	Macros        Macros

	// SourceText is the line an imported plan had for this food, kept when
	// the catalog name differs from it; empty otherwise.
	SourceText string
	// Estimated marks a food and quantity the importer guessed at, from a
	// vague or unmatched line.
	Estimated bool
	// Optional marks a food the plan offers but does not count: it is shown
	// with its own macros and left out of the meal's total.
	Optional bool

	CreatedAt time.Time
}

// FoodLogEntry is one thing a user ate on one day: either a meal-plan meal or
// an ad-hoc ingredient + quantity, never both.
type FoodLogEntry struct {
	ID     uuid.UUID
	UserID uuid.UUID

	LogDate time.Time

	MealID       *uuid.UUID
	IngredientID *uuid.UUID

	// Label is denormalized so the entry still reads sensibly if its source
	// is later deleted.
	Label string

	// QuantityGrams is set only for ad-hoc ingredient logs.
	QuantityGrams *float64
	Macros        Macros

	LoggedAt time.Time
}

// Reminder is a recurring nudge to log a meal at a particular time of day.
type Reminder struct {
	ID     uuid.UUID
	UserID uuid.UUID

	Label string
	// TimeOfDay is "HH:MM", 24-hour, zero-padded so string comparison sorts
	// and compares correctly.
	TimeOfDay string
	// DaysOfWeek uses time.Weekday's numbering: 0=Sunday .. 6=Saturday.
	DaysOfWeek []int
	Enabled    bool

	// LastFiredLocalDate is set when DueNow returns this reminder, so it does
	// not fire twice on the same local day.
	LastFiredLocalDate *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// DueOn reports whether the reminder is scheduled for the given weekday and
// its time has arrived by nowHHMM. It does not check LastFiredLocalDate —
// that idempotency check happens where "today" is known, in the repository
// query and the service that calls it.
func (r Reminder) DueOn(day time.Weekday, nowHHMM string) bool {
	if !r.Enabled {
		return false
	}

	scheduled := false
	for _, d := range r.DaysOfWeek {
		if d == int(day) {
			scheduled = true
			break
		}
	}
	if !scheduled {
		return false
	}

	return r.TimeOfDay <= nowHHMM
}
