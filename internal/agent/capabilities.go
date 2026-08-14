package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/exercises"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

// resultLimit caps how many rows a search hands back to a model.
//
// Small on purpose. These results are prompt tokens on the next turn, and a
// model given fifty exercises picks no better than one given eight.
const resultLimit = 8

// Services is what the capabilities need. Every field is the concrete service
// from its slice: this package is composition, not another layer of interfaces
// over things that already have one caller.
type Services struct {
	Exercises   *exercises.Service
	Calculator  *calculator.Service
	Goals       *goals.Service
	Ingredients *meals.IngredientService
	FoodLog     *meals.FoodLogService
}

// Build registers every capability North exposes to a model.
//
// One list, read by both the coach's chat loop and the MCP server. A tool
// added here appears in both without anything else being touched, which is the
// entire reason this package exists.
func Build(svc Services) *Registry {
	r := NewRegistry()

	if svc.Exercises != nil {
		r.Register(searchExercises(svc.Exercises), getExercise(svc.Exercises))
	}
	if svc.Calculator != nil {
		r.Register(calculateMacros(svc.Calculator))
	}
	if svc.Goals != nil {
		r.Register(listGoals(svc.Goals))
	}
	if svc.Ingredients != nil {
		r.Register(searchIngredients(svc.Ingredients))
	}
	if svc.FoodLog != nil {
		r.Register(todaysNutrition(svc.FoodLog))
	}

	return r
}

// ---------------------------------------------------------------------------
// Exercises
// ---------------------------------------------------------------------------

func searchExercises(svc *exercises.Service) Capability {
	type args struct {
		Query     string `json:"query"`
		Muscle    string `json:"muscle"`
		Equipment string `json:"equipment"`
	}

	return Capability{
		Tool: ai.Tool{
			Name: "search_exercises",
			Description: "Find exercises in North's catalog, by name, by the muscle they train, or by the equipment they need. " +
				"Use this before recommending an exercise, so the muscles you describe are the catalog's and not your own recollection.",
			Parameters: ai.Object("search terms; all are optional, but give at least one", map[string]*ai.Schema{
				"query":     ai.String("part of an exercise name, such as 'squat'"),
				"muscle":    ai.Enum("the muscle group it should train", plan.MuscleGroups...),
				"equipment": ai.String("equipment available, such as 'dumbbell', 'barbell', or 'none' for bodyweight"),
			}, "query", "muscle", "equipment"),
		},
		ReadOnly: true,
		Invoke: func(ctx context.Context, _ uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}

			filter := exercises.Filter{Query: in.Query, Muscle: in.Muscle, Limit: resultLimit}
			if in.Equipment != "" {
				filter.Equipment = []string{in.Equipment}
			}

			found, total, err := svc.Search(ctx, filter)
			if err != nil {
				return "", err
			}
			if len(found) == 0 {
				return "", nil
			}

			var b strings.Builder
			fmt.Fprintf(&b, "%d matches, showing %d:\n", total, len(found))
			for _, e := range found {
				fmt.Fprintf(&b, "- %s\n", e.Line())
			}
			return b.String(), nil
		},
	}
}

func getExercise(svc *exercises.Service) Capability {
	type args = coach.ExerciseArgs

	return Capability{
		Tool: ai.Tool{
			Name:        coach.ToolGetExercise,
			Description: "Read one catalog exercise in full: how to perform it, what it needs, and every muscle it trains.",
			Parameters: ai.Object("which exercise", map[string]*ai.Schema{
				"slug": ai.String("the exercise's slug, as returned by search_exercises"),
			}, "slug"),
		},
		ReadOnly: true,
		Invoke: func(ctx context.Context, _ uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}

			e, err := svc.GetBySlug(ctx, in.Slug)
			if err != nil {
				return "", err
			}

			var b strings.Builder
			fmt.Fprintf(&b, "%s (%s, %s)\n", e.Name, e.Category, e.Difficulty)
			fmt.Fprintf(&b, "Equipment: %s\n", e.Equipment)
			fmt.Fprintf(&b, "Primary muscles: %s\n", join(e.Primary))
			if len(e.Secondary) > 0 {
				fmt.Fprintf(&b, "Secondary muscles: %s\n", join(e.Secondary))
			}
			if e.Instructions != "" {
				fmt.Fprintf(&b, "How to perform it: %s\n", e.Instructions)
			}
			return b.String(), nil
		},
	}
}

// ---------------------------------------------------------------------------
// Calculator
// ---------------------------------------------------------------------------

func calculateMacros(svc *calculator.Service) Capability {
	type args struct {
		ActivityLevel string `json:"activity_level"`
		Goal          string `json:"goal"`
		MacroSplit    string `json:"macro_split"`
	}

	return Capability{
		Tool: ai.Tool{
			Name: "calculate_macros",
			Description: "Work out this person's daily calorie and macro target from the biometrics they have recorded, and save it as their current plan. " +
				"Use this instead of doing the arithmetic yourself — it uses the same Mifflin-St Jeor calculation as the app, so the number matches what they see. " +
				"It fails if they have not recorded their weight, height, and date of birth.",
			Parameters: ai.Object("the choices behind the target", map[string]*ai.Schema{
				"activity_level": ai.Enum("how much they train", calculator.ActivityLevels...),
				"goal":           ai.Enum("what they are trying to do with their weight", calculator.Goals...),
				"macro_split":    ai.Enum("how the calories divide between protein, fat, and carbohydrate", calculator.Splits...),
			}, "activity_level", "goal", "macro_split"),
		},
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}

			// Empty fields are filled by calculator.Validate with the
			// middle-of-the-road option, so a partially-specified call still
			// produces a plan rather than an argument error.
			p, err := svc.Generate(ctx, userID, calculator.Input{
				ActivityLevel: in.ActivityLevel,
				Goal:          in.Goal,
				MacroSplit:    in.MacroSplit,
			})
			if err != nil {
				return "", err
			}
			return p.Summary(), nil
		},
	}
}

// ---------------------------------------------------------------------------
// Goals
// ---------------------------------------------------------------------------

func listGoals(svc *goals.Service) Capability {
	return Capability{
		Tool: ai.Tool{
			Name:        "list_goals",
			Description: "List the goals this person is currently working towards. Use this before giving advice that assumes what they are training for.",
			Parameters:  ai.Object("no arguments", map[string]*ai.Schema{}),
		},
		ReadOnly: true,
		Invoke: func(ctx context.Context, userID uuid.UUID, _ json.RawMessage) (string, error) {
			active, err := svc.ListActive(ctx, userID)
			if err != nil {
				return "", err
			}
			if len(active) == 0 {
				return "", nil
			}

			var b strings.Builder
			for _, goal := range active {
				fmt.Fprintf(&b, "- %s\n", goal.Summary())
			}
			return b.String(), nil
		},
	}
}

// ---------------------------------------------------------------------------
// Nutrition
// ---------------------------------------------------------------------------

func searchIngredients(svc *meals.IngredientService) Capability {
	type args struct {
		Query string `json:"query"`
	}

	return Capability{
		Tool: ai.Tool{
			Name: "search_ingredients",
			Description: "Look up foods in North's ingredient database and read their nutrition per 100g. " +
				"Use this rather than quoting figures from memory, so the numbers match what the person would see if they logged it.",
			Parameters: ai.Object("what to look for", map[string]*ai.Schema{
				"query": ai.String("part of a food's name, such as 'chicken'"),
			}, "query"),
		},
		ReadOnly: true,
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}

			found, err := svc.Search(ctx, userID, in.Query, resultLimit)
			if err != nil {
				return "", err
			}
			if len(found) == 0 {
				return "", nil
			}

			var b strings.Builder
			b.WriteString("Per 100g:\n")
			for _, i := range found {
				fmt.Fprintf(&b, "- %s: %.0f kcal, %.1fg protein, %.1fg fat, %.1fg carbs\n",
					i.Name, i.Per100g.Calories, i.Per100g.ProteinG, i.Per100g.FatG, i.Per100g.CarbG)
			}
			return b.String(), nil
		},
	}
}

func todaysNutrition(svc *meals.FoodLogService) Capability {
	return Capability{
		Tool: ai.Tool{
			Name:        "todays_nutrition",
			Description: "Read what this person has eaten today and the totals so far. Use this before commenting on how their day is going.",
			Parameters:  ai.Object("no arguments", map[string]*ai.Schema{}),
		},
		ReadOnly: true,
		Invoke: func(ctx context.Context, userID uuid.UUID, _ json.RawMessage) (string, error) {
			today := time.Now()

			entries, err := svc.Day(ctx, userID, today)
			if err != nil {
				return "", err
			}
			if len(entries) == 0 {
				return "Nothing logged today.", nil
			}

			totals, err := svc.DailyTotals(ctx, userID, today)
			if err != nil {
				return "", err
			}

			var b strings.Builder
			for _, entry := range entries {
				fmt.Fprintf(&b, "- %s: %.0f kcal\n", entry.Label, entry.Macros.Calories)
			}
			fmt.Fprintf(&b, "Total: %.0f kcal, %.0fg protein, %.0fg fat, %.0fg carbs.\n",
				totals.Calories, totals.ProteinG, totals.FatG, totals.CarbG)
			return b.String(), nil
		},
	}
}

func join(values []string) string { return strings.Join(values, ", ") }
