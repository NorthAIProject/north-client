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
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/documents"
	"github.com/NorthAIProject/north-client/internal/exercises"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/notifications"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts"
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
	Exercises     *exercises.Service
	Calculator    *calculator.Service
	Goals         *goals.Service
	Ingredients   *meals.IngredientService
	FoodLog       *meals.FoodLogService
	CheckIns      *checkins.Service
	Documents     *documents.Service
	Workouts      *workouts.Service
	Notifications *notifications.Service

	// Users resolves the account a call runs as.
	//
	// Needed because some services take the whole users.User rather than an id:
	// a check-in is stored against the person's local date, so writing one
	// requires knowing their timezone. Capabilities receive only a user id — by
	// design, since it comes from the session and nothing in the arguments can
	// influence it — so the record is loaded here.
	Users *users.Service
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
		r.Register(listGoals(svc.Goals), createGoal(svc.Goals), addGoalUpdate(svc.Goals))
	}
	if svc.CheckIns != nil && svc.Users != nil {
		// Both, because writing a check-in needs the person's timezone and
		// that lives on the user record. Registered without Users, the tool
		// would exist and fail on every call.
		r.Register(createCheckIn(svc.CheckIns, svc.Users))
	}
	if svc.Documents != nil {
		r.Register(searchDocs(svc.Documents))
	}
	if svc.Workouts != nil {
		r.Register(getWorkoutPlan(svc.Workouts))
	}
	if svc.Ingredients != nil {
		r.Register(searchIngredients(svc.Ingredients))
	}
	if svc.FoodLog != nil {
		r.Register(todaysNutrition(svc.FoodLog))
	}
	if svc.Notifications != nil {
		r.Register(listAlerts(svc.Notifications), setAlert(svc.Notifications))
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

// ---------------------------------------------------------------------------
// Writes
//
// Everything below changes something. None of them carry ReadOnly, which is
// what makes the coach stop and ask before running one — see the confirmation
// flow in internal/coach. The MCP surface publishes the same annotation and
// leaves the decision to the client.
// ---------------------------------------------------------------------------

func createGoal(svc *goals.Service) Capability {
	type args struct {
		Title      string `json:"title"`
		Motivation string `json:"motivation"`
		Success    string `json:"success"`
		Category   string `json:"category"`
	}

	return Capability{
		Tool: ai.Tool{
			Name: "create_goal",
			Description: "Create a new goal for this person. Use it when they have said what they want to work towards and agreed to track it — " +
				"not to record a passing wish. The title is what they will see in their list, so write it the way they said it.",
			Parameters: ai.Object("the goal to create", map[string]*ai.Schema{
				"title":      ai.String("what they are working towards, in their own words"),
				"motivation": ai.String("why it matters to them; optional"),
				"success":    ai.String("how they will know they have got there; optional"),
				"category":   ai.String("a short grouping such as strength, health, or work; optional"),
			}, "title"),
		},
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}

			// TargetDate is left unset rather than guessed. A deadline the
			// person did not give is one they would have to find and correct.
			goal, err := svc.Create(ctx, userID, goals.Input{
				Title:      in.Title,
				Motivation: in.Motivation,
				Success:    in.Success,
				Category:   in.Category,
			})
			if err != nil {
				return "", err
			}
			return "Created the goal: " + goal.Summary(), nil
		},
	}
}

func addGoalUpdate(svc *goals.Service) Capability {
	type args struct {
		GoalTitle string `json:"goal_title"`
		Note      string `json:"note"`
		// A pointer so "not given" is distinguishable from "zero percent" —
		// the service treats nil as "leave the figure alone".
		Progress *int `json:"progress,omitempty"`
	}

	return Capability{
		Tool: ai.Tool{
			Name:        "add_goal_update",
			Description: "Record progress against one of the user's goals. The goal is named by title, not by ID.",
			Parameters: ai.Object("the progress to record", map[string]*ai.Schema{
				"goal_title": ai.String("the goal to update, matched by title"),
				"note":       ai.String("what happened, in the user's own terms"),
				"progress":   ai.Integer("completion percentage from 0 to 100"),
			}, "goal_title", "note"),
		},
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}

			all, err := svc.List(ctx, userID)
			if err != nil {
				return "", err
			}

			// By title, never by id. A model that could name a UUID would be
			// one prompt away from writing to somebody else's goal; a title
			// only resolves within this person's own list.
			goal, err := pickGoal(all, in.GoalTitle)
			if err != nil {
				return "", err
			}

			if _, err = svc.AddUpdate(ctx, goal.ID, userID, in.Note, in.Progress); err != nil {
				return "", err
			}
			return fmt.Sprintf("Recorded against %q: %s", goal.Title, in.Note), nil
		},
	}
}

func createCheckIn(svc *checkins.Service, userSvc *users.Service) Capability {
	type args struct {
		Mood       int    `json:"mood"`
		Energy     int    `json:"energy"`
		Wins       string `json:"wins"`
		Challenges string `json:"challenges"`
		Notes      string `json:"notes"`
	}

	return Capability{
		Tool: ai.Tool{
			Name: "create_check_in",
			Description: "Record today's check-in. Writing again on the same day replaces that day's entry " +
				"rather than adding a second one, so this is safe to call twice.",
			Parameters: ai.Object("today's check-in", map[string]*ai.Schema{
				"mood":       ai.Integer("how the user feels, 1 (worst) to 5 (best)"),
				"energy":     ai.Integer("the user's energy level, 1 (lowest) to 5 (highest)"),
				"wins":       ai.String("what went well"),
				"challenges": ai.String("what got in the way"),
				"notes":      ai.String("anything else worth telling the coach"),
			}, "mood", "energy"),
		},
		// An upsert: the second call of the day corrects the first rather than
		// adding to it, which is what makes a retry safe.
		Idempotent: true,
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}

			// The whole record, not just the id: a check-in is filed under the
			// person's local date, so this needs their timezone.
			user, err := userSvc.ByID(ctx, userID)
			if err != nil {
				return "", err
			}

			entry, err := svc.UpsertToday(ctx, user, checkins.Input{
				Mood:       in.Mood,
				Energy:     in.Energy,
				Wins:       in.Wins,
				Challenges: in.Challenges,
				Notes:      in.Notes,
			})
			if err != nil {
				return "", err
			}

			streak, err := svc.Streak(ctx, user)
			if err != nil {
				// The check-in is saved. A streak we could not count is not
				// worth reporting as a failure.
				return fmt.Sprintf("Logged today's check-in: mood %d, energy %d.", entry.Mood, entry.Energy), nil
			}
			return fmt.Sprintf("Logged today's check-in: mood %d, energy %d. That is a %d-day streak.",
				entry.Mood, entry.Energy, streak), nil
		},
	}
}

// ---------------------------------------------------------------------------
// Knowledge and training
// ---------------------------------------------------------------------------

func searchDocs(svc *documents.Service) Capability {
	type args struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}

	return Capability{
		Tool: ai.Tool{
			Name: "search_documents",
			Description: "Search the passages of the user's own notes and uploaded documents. Returns each " +
				"passage with the document it came from, the heading above it, its line range, and a " +
				"citable chunk id. Quote the chunk id when using a passage.",
			Parameters: ai.Object("what to look for", map[string]*ai.Schema{
				"query": ai.String("what to look for in the user's own notes and documents"),
				"limit": ai.Integer("maximum passages, default 6"),
			}, "query"),
		},
		ReadOnly: true,
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}

			hits, err := svc.Search(ctx, userID, in.Query, in.Limit)
			if err != nil {
				return "", err
			}
			if len(hits) == 0 {
				return "", nil
			}

			var b strings.Builder
			for _, h := range hits {
				// The ref, not the internal document id: this is the handle
				// that still resolves in a stored reply months from now.
				fmt.Fprintf(&b, "- [%s] %s (lines %d-%d)\n  %s\n",
					coach.ChunkRef(h.ChunkID), h.Label(), h.StartLine, h.EndLine, h.Content)
			}
			return b.String(), nil
		},
	}
}

func getWorkoutPlan(svc *workouts.Service) Capability {
	return Capability{
		Tool: ai.Tool{
			Name: "get_workout_plan",
			Description: "Read this person's current training plan: the days, the focus of each, and the exercises on them. " +
				"Use it before advising on training, so the advice fits the plan they are actually following.",
			Parameters: ai.Object("no arguments", map[string]*ai.Schema{}),
		},
		ReadOnly: true,
		Invoke: func(ctx context.Context, userID uuid.UUID, _ json.RawMessage) (string, error) {
			stored, err := svc.LatestPlan(ctx, userID)
			if err != nil {
				if apperr.Is(err, apperr.ErrNotFound) {
					// Not an error the model should apologise for. They simply
					// have no plan yet, and saying so lets it offer to build one.
					return "", nil
				}
				return "", err
			}

			var b strings.Builder
			fmt.Fprintf(&b, "%s (%d weeks)\n", stored.Plan.Name, stored.Plan.WeeksTotal)
			for _, day := range stored.Plan.Days {
				fmt.Fprintf(&b, "- %s — %s:", day.Weekday, day.Focus)
				for i, exercise := range day.Exercises {
					if i > 0 {
						b.WriteString(",")
					}
					fmt.Fprintf(&b, " %s %dx%s", exercise.Name, exercise.Sets, exercise.Reps)
				}
				b.WriteString("\n")
			}
			return b.String(), nil
		},
	}
}

// pickGoal resolves a goal by the title a model used.
//
// Moved here from the MCP surface along with add_goal_update, because it is
// what makes naming a goal by title safe: a title only ever resolves within one
// person's own list, where a UUID would resolve anywhere.
//
// An ambiguous title is an error rather than a guess. Picking the first of
// several matches would write to the wrong goal silently, and the model can ask
// when it is told what the choices are.
func pickGoal(all []goals.Goal, title string) (goals.Goal, error) {
	needle := strings.TrimSpace(strings.ToLower(title))

	var hits []goals.Goal
	for _, g := range all {
		if needle == "" || strings.Contains(strings.ToLower(g.Title), needle) {
			hits = append(hits, g)
		}
	}

	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return goals.Goal{}, fmt.Errorf("no goal matches %q", title)
	default:
		names := make([]string, 0, len(hits))
		for _, g := range hits {
			names = append(names, g.Title)
		}
		return goals.Goal{}, fmt.Errorf("%q matches several goals (%v); be more specific", title, names)
	}
}
