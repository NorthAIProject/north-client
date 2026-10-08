package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/meals/meal"
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

// maxPlanMeals and maxMealIngredients bound one day of one call. A day
// rarely has more than six eating occasions, and a meal of twenty ingredients
// is a recipe the catalog lookup would not resolve reliably anyway.
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
	type dayArg struct {
		Meals []mealArg `json:"meals"`
	}
	type args struct {
		Name        string        `json:"name"`
		Description string        `json:"description"`
		Objective   string        `json:"objective"`
		PlanType    meal.PlanType `json:"plan_type"`
		Days        []dayArg      `json:"days"`
	}

	planTypes := make([]string, len(meal.CarbBands))
	shares := make([]string, len(meal.CarbBands))
	for i, b := range meal.CarbBands {
		planTypes[i] = string(b.Type)
		shares[i] = fmt.Sprintf("%s %.1f%%", b.Type, b.DefaultPct())
	}

	return Capability{
		Tool: ai.Tool{
			Name: "create_meal_plan",
			Description: "Create a meal plan for this person: one to seven days from Monday, each with named meals in eating order made of " +
				"catalog ingredients by weight. Use it when they ask for a meal plan or agree to one you proposed. " +
				"Every day is held to their macro target from the calculator, with carbs a share of their carb target set by " +
				"plan_type (" + strings.Join(shares, ", ") + "); a day over its protein, carbs or fat is " +
				"refused with the amounts, and nothing is saved — shrink portions and try again. Without a target the plan cannot " +
				"be made: offer calculate_macros first. Use plain ingredient names such as 'oats' or 'chicken breast' — each is " +
				"looked up in the catalog, and an unknown or ambiguous one stops the whole plan. Use search_ingredients first when " +
				"unsure of a name.",
			Parameters: ai.Object("the plan to create", map[string]*ai.Schema{
				"name":        ai.String("what the plan is called, such as 'Cutting — 2,100 kcal'"),
				"description": ai.String("a sentence on what the plan is for; optional"),
				"objective":   ai.Enum("the aim, when there is one", "cutting", "maintenance", "bulking"),
				"plan_type":   ai.Enum("the carb level; mid_carb when they have not said", planTypes...),
				"days": ai.Array("the plan's days, Monday first", ai.Object("one day", map[string]*ai.Schema{
					"meals": ai.Array("the day's meals in eating order", ai.Object("one meal", map[string]*ai.Schema{
						"name": ai.String("the meal, such as 'Breakfast'"),
						"ingredients": ai.Array("what goes in it", ai.Object("one ingredient", map[string]*ai.Schema{
							"food":  ai.String("the ingredient's plain name"),
							"grams": ai.Number("how much, in grams"),
						}, "food", "grams")),
					}, "name", "ingredients")),
				}, "meals")),
			}, "name", "days"),
		},
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}
			if len(in.Days) == 0 || len(in.Days) > meal.MaxDays {
				return "", apperr.Wrap(apperr.ErrValidation, "a meal plan needs between 1 and %d days", meal.MaxDays)
			}
			if in.PlanType == "" {
				in.PlanType = meal.MidCarb
			}

			// Every name is resolved before anything is written: a plan with
			// the dinner missing because "salmon filet" did not match would be
			// worse than asking again.
			days := make([]meals.DayDraft, len(in.Days))
			for d, day := range in.Days {
				if len(day.Meals) == 0 || len(day.Meals) > maxPlanMeals {
					return "", apperr.Wrap(apperr.ErrValidation, "day %d needs between 1 and %d meals", d+1, maxPlanMeals)
				}
				for i, m := range day.Meals {
					if strings.TrimSpace(m.Name) == "" || len(m.Ingredients) == 0 || len(m.Ingredients) > maxMealIngredients {
						return "", apperr.Wrap(apperr.ErrValidation,
							"day %d, meal %d needs a name and between 1 and %d ingredients", d+1, i+1, maxMealIngredients)
					}
					draft := meals.MealDraft{Name: m.Name}
					for _, ing := range m.Ingredients {
						if ing.Grams <= 0 || ing.Grams > 2000 {
							return "", apperr.Wrap(apperr.ErrValidation, "%q needs a weight between 1 and 2000 g", ing.Food)
						}
						match, matchErr := matchIngredient(ctx, ingredients, userID, ing.Food)
						if matchErr != nil {
							return "", matchErr
						}
						draft.Portions = append(draft.Portions, meals.MealIngredientInput{IngredientID: match.ID, QuantityGrams: ing.Grams})
					}
					days[d].Meals = append(days[d].Meals, draft)
				}
			}

			// Easy mode, so a day over the target is refused rather than
			// offered for confirmation: the model has no one to confirm with.
			stored, err := plans.CreatePlan(ctx, userID, meals.MealPlanInput{
				Name: in.Name, Description: in.Description, Objective: in.Objective,
				Settings: meal.PlanSettings{Type: in.PlanType, Mode: meal.Easy},
				DayCount: len(in.Days),
			}, days, false)
			if err != nil {
				return "", err
			}

			var b strings.Builder
			fmt.Fprintf(&b, "Created the meal plan %q (%s, %d days).", stored.Name, stored.Settings.Type.Label(), len(stored.Days))
			for _, day := range stored.Days {
				total := day.Consumed()
				fmt.Fprintf(&b, "\n%s: %.0f kcal, %.0f g protein, %.0f g carbs, %.0f g fat.",
					day.Weekday, total.Calories, total.ProteinG, total.CarbG, total.FatG)
				for _, m := range day.Meals {
					names := make([]string, len(m.Ingredients))
					for j, ing := range m.Ingredients {
						names[j] = fmt.Sprintf("%.0f g %s", ing.QuantityGrams, ing.IngredientName)
					}
					fmt.Fprintf(&b, "\n  %s (%.0f kcal): %s", m.Name, m.TotalMacros.Calories, strings.Join(names, ", "))
				}
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
