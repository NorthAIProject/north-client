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

// MealPlan groups a set of meals around a stated objective.
type MealPlan struct {
	ID     uuid.UUID
	UserID uuid.UUID

	Name          string
	Description   string
	Objective     string
	ActivityLevel string
	Gender        string

	// TotalMacros is a cache kept current by the service on every ingredient
	// add/remove, not re-summed on every read.
	TotalMacros Macros
	Meals       []Meal

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Meal is one meal within a plan (breakfast, lunch, ...), ordered by
// MealNumber.
type Meal struct {
	ID         uuid.UUID
	MealPlanID uuid.UUID

	MealNumber int
	Name       string

	TotalMacros Macros
	Ingredients []MealIngredient

	CreatedAt time.Time
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
