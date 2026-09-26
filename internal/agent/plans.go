package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/meals"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts"
)

// Building plans from the chat.
//
// "Make me a meal plan for a cut" and "put together a four-day split" are
// things people ask a coach, in the web chat and on Telegram alike. Both
// tools write, so each shows an approval card carrying the whole plan before
// anything is stored (see coach.writingCalls).

// maxPlanMeals and maxMealIngredients bound one call. A day rarely has more
// than six eating occasions, and a meal of twenty ingredients is a recipe
// the catalog lookup would not resolve reliably anyway.
const (
	maxPlanMeals       = 8
	maxMealIngredients = 15
)

func createMealPlan(plans *meals.MealPlanService, ingredients *meals.IngredientService) Capability {
	type ingredientArg struct {
		Food  string  `json:"food"`
		Grams float64 `json:"grams"`
	}
	type mealArg struct {
		Name        string          `json:"name"`
		Ingredients []ingredientArg `json:"ingredients"`
	}
	type args struct {
		Name        string    `json:"name"`
		Description string    `json:"description"`
		Objective   string    `json:"objective"`
		Meals       []mealArg `json:"meals"`
	}

	return Capability{
		Tool: ai.Tool{
			Name: "create_meal_plan",
			Description: "Create a meal plan for this person: named meals in eating order, each made of catalog ingredients by weight. " +
				"Use it when they ask for a meal plan or agree to one you proposed. Build it against their macro targets " +
				"(calculate_macros) and dietary preferences, and use plain ingredient names such as 'oats' or 'chicken breast' — " +
				"each is looked up in the catalog, and an unknown or ambiguous one stops the whole plan so nothing half-made is saved. " +
				"Use search_ingredients first when unsure of a name.",
			Parameters: ai.Object("the plan to create", map[string]*ai.Schema{
				"name":        ai.String("what the plan is called, such as 'Cutting — 2,100 kcal'"),
				"description": ai.String("a sentence on what the plan is for; optional"),
				"objective":   ai.Enum("the aim, when there is one", "cutting", "maintenance", "bulking"),
				"meals": ai.Array("the meals in eating order", ai.Object("one meal", map[string]*ai.Schema{
					"name": ai.String("the meal, such as 'Breakfast'"),
					"ingredients": ai.Array("what goes in it", ai.Object("one ingredient", map[string]*ai.Schema{
						"food":  ai.String("the ingredient's plain name"),
						"grams": ai.Number("how much, in grams"),
					}, "food", "grams")),
				}, "name", "ingredients")),
			}, "name", "meals"),
		},
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}
			if len(in.Meals) == 0 || len(in.Meals) > maxPlanMeals {
				return "", apperr.Wrap(apperr.ErrValidation, "a meal plan needs between 1 and %d meals", maxPlanMeals)
			}

			// Every name is resolved before anything is written: a plan with
			// the dinner missing because "salmon filet" did not match would be
			// worse than asking again.
			type resolved struct {
				id    uuid.UUID
				grams float64
			}
			planned := make([][]resolved, len(in.Meals))
			for i, m := range in.Meals {
				if strings.TrimSpace(m.Name) == "" || len(m.Ingredients) == 0 || len(m.Ingredients) > maxMealIngredients {
					return "", apperr.Wrap(apperr.ErrValidation,
						"meal %d needs a name and between 1 and %d ingredients", i+1, maxMealIngredients)
				}
				for _, ing := range m.Ingredients {
					if ing.Grams <= 0 || ing.Grams > 2000 {
						return "", apperr.Wrap(apperr.ErrValidation, "%q needs a weight between 1 and 2000 g", ing.Food)
					}
					match, matchErr := matchIngredient(ctx, ingredients, userID, ing.Food)
					if matchErr != nil {
						return "", matchErr
					}
					planned[i] = append(planned[i], resolved{id: match.ID, grams: ing.Grams})
				}
			}

			plan, err := plans.CreatePlan(ctx, userID, meals.MealPlanInput{
				Name: in.Name, Description: in.Description, Objective: in.Objective,
			})
			if err != nil {
				return "", err
			}
			// A failure part-way removes the plan rather than leaving a
			// fragment in their list.
			fail := func(err error) (string, error) {
				_ = plans.DeletePlan(context.WithoutCancel(ctx), plan.ID, userID)
				return "", err
			}
			for i, m := range in.Meals {
				meal, mealErr := plans.AddMeal(ctx, plan.ID, userID, meals.MealInput{Name: m.Name, MealNumber: i + 1})
				if mealErr != nil {
					return fail(mealErr)
				}
				for _, ing := range planned[i] {
					if _, ingErr := plans.AddIngredient(ctx, meal.ID, userID, meals.MealIngredientInput{
						IngredientID: ing.id, QuantityGrams: ing.grams,
					}); ingErr != nil {
						return fail(ingErr)
					}
				}
			}

			stored, err := plans.GetPlan(ctx, plan.ID, userID)
			if err != nil {
				return "", err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "Created the meal plan %q: %.0f kcal, %.0f g protein, %.0f g carbs, %.0f g fat a day.",
				stored.Name, stored.TotalMacros.Calories, stored.TotalMacros.ProteinG, stored.TotalMacros.CarbG, stored.TotalMacros.FatG)
			for _, meal := range stored.Meals {
				names := make([]string, len(meal.Ingredients))
				for j, ing := range meal.Ingredients {
					names[j] = fmt.Sprintf("%.0f g %s", ing.QuantityGrams, ing.IngredientName)
				}
				fmt.Fprintf(&b, "\n%s (%.0f kcal): %s", meal.Name, meal.TotalMacros.Calories, strings.Join(names, ", "))
			}
			return b.String(), nil
		},
	}
}

func createWorkoutPlan(svc *workouts.Service, userSvc *users.Service) Capability {
	type args struct {
		Goal           string   `json:"goal"`
		Experience     string   `json:"experience"`
		DaysPerWeek    int      `json:"days_per_week"`
		SessionMinutes int      `json:"session_minutes"`
		Equipment      []string `json:"equipment"`
		Limitations    string   `json:"limitations"`
	}

	return Capability{
		Tool: ai.Tool{
			Name: "create_workout_plan",
			Description: "Generate and save a new training plan from what the person told you: their goal, experience, how many days a week " +
				"and how long each session, the equipment they have, and any injuries or limits. It replaces the plan they follow now " +
				"(the old one is kept in history). Ask for anything missing rather than guessing days or equipment. " +
				"To change one exercise in the current plan, use swap/add/remove_workout_exercise instead.",
			Parameters: ai.Object("what the plan must fit", map[string]*ai.Schema{
				"goal":            ai.String("what they are training for, in their words"),
				"experience":      ai.Enum("training experience", "beginner", "intermediate", "advanced"),
				"days_per_week":   ai.Integer("training days a week, 1 to 7"),
				"session_minutes": ai.Integer("minutes per session, 10 to 240"),
				"equipment":       ai.Array("equipment they have, such as barbell, dumbbell, kettlebell, bands, machines, pull-up bar, or none", ai.String("one piece of equipment")),
				"limitations":     ai.String("injuries or movements to avoid; optional"),
			}, "goal", "experience", "days_per_week", "session_minutes", "equipment"),
		},
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}
			user, err := userSvc.ByID(ctx, userID)
			if err != nil {
				return "", err
			}
			stored, err := svc.CreatePlan(ctx, user, workouts.Intake{
				Goal: in.Goal, Experience: in.Experience, DaysPerWeek: in.DaysPerWeek,
				SessionMinutes: in.SessionMinutes, Equipment: in.Equipment, Limitations: in.Limitations,
			})
			if err != nil {
				return "", err
			}
			return "Created and saved the new training plan:\n" + stored.Plan.Summary(), nil
		},
	}
}
