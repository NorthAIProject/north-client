// Package meals owns North's nutrition domain: ingredients, diet
// preferences, meal plans, food logging, progress tracking against the
// calculator's macro goal, goal recommendations, and meal reminders.
package meals

import "github.com/NorthAIProject/north-client/internal/meals/meal"

// The domain shapes live in a leaf package so the services and any future
// template that renders one do not import each other.
type (
	Macros         = meal.Macros
	Ingredient     = meal.Ingredient
	Diet           = meal.Diet
	MealPlan       = meal.MealPlan
	Meal           = meal.Meal
	MealIngredient = meal.MealIngredient
	FoodLogEntry   = meal.FoodLogEntry
	Reminder       = meal.Reminder
	DayTarget      = meal.DayTarget
	Overage        = meal.Overage
)

const (
	PlanTypeNoCarb   = meal.PlanTypeNoCarb
	PlanTypeLowCarb  = meal.PlanTypeLowCarb
	PlanTypeMidCarb  = meal.PlanTypeMidCarb
	PlanTypeHighCarb = meal.PlanTypeHighCarb
	PlanTypeCustom   = meal.PlanTypeCustom
)

var (
	PlanTypes        = meal.PlanTypes
	PlanTypeLabel    = meal.PlanTypeLabel
	CarbRange        = meal.CarbRange
	DefaultCarbPct   = meal.DefaultCarbPct
	CheckOverage     = meal.CheckOverage
	ResolveDayTarget = meal.ResolveDayTarget
)

const (
	CategoryProtein   = meal.CategoryProtein
	CategoryCarb      = meal.CategoryCarb
	CategoryFat       = meal.CategoryFat
	CategoryDairy     = meal.CategoryDairy
	CategoryVegetable = meal.CategoryVegetable
	CategoryFruit     = meal.CategoryFruit
	CategoryBeverage  = meal.CategoryBeverage
	CategorySnack     = meal.CategorySnack
	CategoryOther     = meal.CategoryOther
)

var Categories = meal.Categories

