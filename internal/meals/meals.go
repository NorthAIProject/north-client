// Package meals owns North's nutrition domain: ingredients, diet
// preferences, meal plans, food logging, progress tracking against the
// calculator's macro goal, goal recommendations, and meal reminders.
package meals

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/meals/meal"
)

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

// Repository is the persistence the meal services need: IngredientService,
// DietPreferenceService, MealPlanService, FoodLogService and
// MealReminderService all hold one. They name it rather than the Postgres type
// so the database stays a detail of the composition root: NewRepository is
// what the process wires, and a test can hand them anything that keeps
// ingredients, diets, meal plans, food logs and reminders.
type Repository interface {
	ListDiets(ctx context.Context) ([]Diet, error)
	UserDiets(ctx context.Context, userID uuid.UUID) ([]Diet, error)
	SetUserDiets(ctx context.Context, userID uuid.UUID, dietIDs []uuid.UUID) error
	AddUserDiet(ctx context.Context, userID, dietID uuid.UUID) error
	RemoveUserDiet(ctx context.Context, userID, dietID uuid.UUID) error
	InsertFoodLog(ctx context.Context, userID uuid.UUID, logDate time.Time, mealID, ingredientID *uuid.UUID, quantityGrams *float64, label string, macros Macros) (FoodLogEntry, error)
	DeleteFoodLog(ctx context.Context, id, userID uuid.UUID) error
	FoodLogsByDate(ctx context.Context, userID uuid.UUID, date time.Time) ([]FoodLogEntry, error)
	FoodLogsByRange(ctx context.Context, userID uuid.UUID, from, to time.Time) ([]FoodLogEntry, error)
	DailyTotals(ctx context.Context, userID uuid.UUID, date time.Time) (Macros, error)
	CreateIngredient(ctx context.Context, userID uuid.UUID, in Ingredient) (Ingredient, error)
	GetIngredient(ctx context.Context, id, userID uuid.UUID) (Ingredient, error)
	SearchIngredients(ctx context.Context, userID uuid.UUID, query string, limit int) ([]Ingredient, error)
	SharedIngredientNames(ctx context.Context) ([]string, error)
	UpdateIngredient(ctx context.Context, id, userID uuid.UUID, in Ingredient) (Ingredient, error)
	DeleteIngredient(ctx context.Context, id, userID uuid.UUID) error
	CreatePlan(ctx context.Context, userID uuid.UUID, plan NewPlan, days []NewDay) (uuid.UUID, error)
	GetPlan(ctx context.Context, id, userID uuid.UUID) (MealPlan, error)
	ListPlans(ctx context.Context, userID uuid.UUID) ([]MealPlan, error)
	DeletePlan(ctx context.Context, id, userID uuid.UUID) error
	WithPlanLocked(ctx context.Context, planID, userID uuid.UUID, fn func(tx *PlanTx, plan MealPlan) error) error
	PlanIDOfMeal(ctx context.Context, mealID, userID uuid.UUID) (uuid.UUID, error)
	PlanIDOfDay(ctx context.Context, dayID, userID uuid.UUID) (uuid.UUID, error)
	PlanIDOfMealIngredient(ctx context.Context, mealIngredientID, userID uuid.UUID) (uuid.UUID, error)
	LocateMealIngredient(ctx context.Context, mealIngredientID, userID uuid.UUID) (planID, mealID uuid.UUID, err error)
	GetMeal(ctx context.Context, mealID, userID uuid.UUID) (Meal, error)
	CreateReminder(ctx context.Context, userID uuid.UUID, label, timeOfDay string, daysOfWeek []int) (Reminder, error)
	ListReminders(ctx context.Context, userID uuid.UUID) ([]Reminder, error)
	UpdateReminder(ctx context.Context, id, userID uuid.UUID, label, timeOfDay string, daysOfWeek []int) (Reminder, error)
	DeleteReminder(ctx context.Context, id, userID uuid.UUID) error
	SetReminderEnabled(ctx context.Context, id, userID uuid.UUID, enabled bool) (Reminder, error)
	NotYetFiredToday(ctx context.Context, userID uuid.UUID, asOfDate time.Time) ([]Reminder, error)
	MarkFired(ctx context.Context, id uuid.UUID, asOfDate time.Time) error
	CreateMealPlanIntake(ctx context.Context, userID uuid.UUID, in MealPlanIntake) (MealPlanIntake, error)
	LatestMealPlanIntake(ctx context.Context, userID uuid.UUID) (*MealPlanIntake, error)
	SharedCatalog(ctx context.Context) ([]CatalogFood, error)
}
