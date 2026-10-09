package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/NorthAIProject/north-client/internal/stats"

	"github.com/NorthAIProject/north-client/internal/caffeine"
	"github.com/NorthAIProject/north-client/internal/fasting"
	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/lifts"
	"github.com/NorthAIProject/north-client/internal/screentime"
	"github.com/NorthAIProject/north-client/internal/soreness"
	"github.com/NorthAIProject/north-client/internal/supplements"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/documents"
	"github.com/NorthAIProject/north-client/internal/exercises"
	"github.com/NorthAIProject/north-client/internal/exercises/exercise"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/habits"
	"github.com/NorthAIProject/north-client/internal/hydration"
	"github.com/NorthAIProject/north-client/internal/meals"
	"github.com/NorthAIProject/north-client/internal/media"
	"github.com/NorthAIProject/north-client/internal/medications"
	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/planimport"
	"github.com/NorthAIProject/north-client/internal/preferences"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/sleep"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/watches"
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

	// The day's logs. Each is the slice that already owns the table; this
	// package adds no persistence of its own.
	Hydration  *hydration.Service
	Sleep      *sleep.Service
	Habits     *habits.Service
	Biometrics *biometrics.Service

	// Watches stores standing tasks (create_watch). Left nil on surfaces with
	// nobody to confirm the card and no thread to post results into — the MCP
	// server — so the tool simply does not exist there.
	Watches *watches.Service

	// Activity logs a finished workout — the run someone did this morning.
	// Needs Users too, for the timezone a spoken start time is read in.
	Activity *activity.Service

	// Plans built by conversation: a meal plan from catalog ingredients, and
	// a training plan generated from an intake. MealPlans also needs
	// Ingredients; Workouts is shared with the plan-editing tools.
	MealPlans *meals.MealPlanService

	// My Day's trackers. Each needs Users, for the local date it files under.
	Caffeine    *caffeine.Service
	Supplements *supplements.Service
	Fasting     *fasting.Service
	ScreenTime  *screentime.Service
	Soreness    *soreness.Service
	Lifts       *lifts.Service
	// Health takes a blood pressure reading.
	Health *health.Service

	// Stats reads the stats pages (sleep, cardio, eating, patterns).
	Stats *stats.Service

	// Preferences holds the target weight.
	Preferences *preferences.Service

	// Medications is what someone takes and when; see medications.go.
	Medications *medications.Service

	// PlanImport and Media import a plan from a file sent in chat
	// (import_plan_from_attachment). Media finds the file the person sent;
	// PlanImport reads and saves it. Left nil on the MCP server, which has no
	// chat to send a file in, so the tool does not exist there. Documents,
	// when set, keeps the file's advice as a note.
	PlanImport *planimport.Service
	Media      *media.Service

	// SiteURL is the public origin, used to build the absolute asset URLs a
	// tool hands back. Environment-specific on purpose: an agent talking to a
	// laptop should be given that laptop's addresses, not production's.
	//
	// Passed in rather than read from a brand constant so this package keeps
	// pointing only at internal/ — the tool registry is service layer, and the
	// canonical-origin constant in web/shared/ui belongs to the pages.
	SiteURL string

	// Users resolves the account a call runs as.
	//
	// Needed because some services take the whole users.User rather than an id:
	// a check-in is stored against the person's local date, so writing one
	// requires knowing their timezone. Capabilities receive only a user id — by
	// design, since it comes from the session and nothing in the arguments can
	// influence it — so the record is loaded here.
	Users *users.Service
}

// Build registers every capability Khepri exposes to a model.
//
// One list, read by both the coach's chat loop and the MCP server. A tool
// added here appears in both without anything else being touched, which is the
// entire reason this package exists.
func Build(svc Services) *Registry {
	r := NewRegistry()

	if svc.Exercises != nil {
		r.Register(searchExercises(svc.Exercises), getExercise(svc.Exercises, svc.SiteURL))
	}
	if svc.Calculator != nil {
		r.Register(calculateMacros(svc.Calculator))
	}
	if svc.Goals != nil {
		r.Register(listGoals(svc.Goals), createGoal(svc.Goals), addGoalUpdate(svc.Goals), updateGoal(svc.Goals))
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

		// Editing the plan by conversation. Users as well, for the same reason
		// createCheckIn needs it: the edit methods take the whole record, so
		// registered without it these would exist and fail on every call.
		//
		// None is ReadOnly, so each shows an approval card carrying the call
		// and its arguments before it runs — see internal/agent/workout_edits.go.
		if svc.Users != nil {
			r.Register(
				swapWorkoutExercise(svc.Workouts, svc.Users),
				addWorkoutExercise(svc.Workouts, svc.Users),
				removeWorkoutExercise(svc.Workouts, svc.Users),
				setWorkoutPrescription(svc.Workouts, svc.Users),
				moveWorkoutExercise(svc.Workouts, svc.Users),
				setWorkoutStartTime(svc.Workouts, svc.Users),
			)

			// The week, and which plan is followed. See training_week.go.
			r.Register(
				getTrainingWeek(svc.Workouts, svc.Users),
				setTrainingWeek(svc.Workouts, svc.Users),
				setActiveWorkoutPlan(svc.Workouts, svc.Users),
			)
		}
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

	// The day's logs. Users as well for the three that file against a local
	// date, for the same reason createCheckIn needs it: registered without the
	// user record these would exist and fail on every call.
	if svc.Users != nil {
		if svc.Hydration != nil {
			r.Register(logWater(svc.Hydration, svc.Users))
		}
		if svc.Sleep != nil {
			r.Register(logSleep(svc.Sleep, svc.Users))
		}
		if svc.Habits != nil {
			r.Register(completeHabit(svc.Habits, svc.Users), createHabit(svc.Habits, svc.Users), updateHabit(svc.Habits, svc.Users))
		}
		if svc.Activity != nil {
			r.Register(logActivity(svc.Activity, svc.Users))
		}
	}
	if svc.Watches != nil {
		r.Register(createWatch(svc.Watches))
	}
	if svc.Biometrics != nil {
		// No Users: a weight is not filed against a local date.
		r.Register(recordWeight(svc.Biometrics))
	}
	if svc.FoodLog != nil && svc.Ingredients != nil {
		r.Register(logFood(svc.FoodLog, svc.Ingredients))
	}
	if svc.MealPlans != nil && svc.Ingredients != nil {
		r.Register(
			createMealPlan(svc.MealPlans, svc.Ingredients),
			getMealPlan(svc.MealPlans),
			editMealPlan(svc.MealPlans, svc.Ingredients),
		)
		if svc.FoodLog != nil && svc.Users != nil {
			r.Register(logPlannedMeal(svc.MealPlans, svc.FoodLog, svc.Users))
		}
	}
	if svc.PlanImport != nil && svc.Media != nil && svc.Users != nil {
		r.Register(importPlanFromAttachment(svc.PlanImport, svc.Media, svc.Users, svc.Documents))
	}
	if svc.Workouts != nil && svc.Users != nil {
		r.Register(createWorkoutPlan(svc.Workouts, svc.Users))
	}
	if svc.Users != nil {
		if svc.Caffeine != nil {
			r.Register(logCaffeine(svc.Caffeine, svc.Users))
		}
		if svc.Supplements != nil {
			r.Register(logSupplement(svc.Supplements, svc.Users))
		}
		if svc.Fasting != nil {
			r.Register(startFast(svc.Fasting, svc.Users), stopFast(svc.Fasting, svc.Users))
		}
		if svc.ScreenTime != nil {
			r.Register(logScreenTime(svc.ScreenTime, svc.Users))
		}
		if svc.Soreness != nil {
			r.Register(recordSoreness(svc.Soreness, svc.Users))
		}
		if svc.Lifts != nil {
			r.Register(logLiftSet(svc.Lifts, svc.Users), getLiftStats(svc.Lifts, svc.Users), getTrainingContext(svc.Lifts, svc.Users))
		}
	}
	if svc.Health != nil {
		r.Register(recordBloodPressure(svc.Health))
	}
	if svc.Stats != nil && svc.Users != nil {
		r.Register(getStats(svc.Stats, svc.Users))
	}
	if svc.Preferences != nil {
		r.Register(setTargetWeight(svc.Preferences))
	}
	if svc.Medications != nil && svc.Users != nil {
		r.Register(
			listMedications(svc.Medications, svc.Users),
			addMedication(svc.Medications, svc.Users),
			updateMedication(svc.Medications, svc.Users),
			stopMedication(svc.Medications, svc.Users),
			logMedicationDose(svc.Medications, svc.Users),
		)
	}

	// Taking back any of the day's logs, over whichever trackers are wired.
	// See corrections.go.
	if svc.Users != nil {
		if sources := undoSources(svc); len(sources) > 0 {
			r.Register(undoLog(sources, svc.Users))
		}
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
			Description: "Find exercises in Khepri's catalog, by name, by the muscle they train, or by the equipment they need. " +
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

func getExercise(svc *exercises.Service, siteURL string) Capability {
	type args = coach.ExerciseArgs

	return Capability{
		Tool: ai.Tool{
			Name: coach.ToolGetExercise,
			// Says when to call it, not just what it does. An MCP client never
			// sees Khepri's system prompt, so for that surface this sentence is
			// the only thing that will ever ask for the lookup.
			Description: "Read one catalog exercise in full: how to perform it, what it needs, and every muscle it trains. " +
				"Call this before describing how a movement is performed rather than answering from memory, and pass on the video and illustration it returns.",
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

			return describeExercise(e, siteURL), nil
		},
	}
}

// describeExercise renders one catalogue row for a model to read.
//
// Pulled out of the capability so it can be tested without a database, and
// because it is the whole product surface of get_exercise: what this function
// omits, no model on any channel can tell anyone.
func describeExercise(e exercise.Exercise, siteURL string) string {
	var b strings.Builder

	fmt.Fprintf(&b, "%s (%s, %s)\n", e.Name, e.Category, e.Difficulty)
	fmt.Fprintf(&b, "Equipment: %s\n", e.Equipment)
	fmt.Fprintf(&b, "Primary muscles: %s\n", join(e.Primary))
	if len(e.Secondary) > 0 {
		fmt.Fprintf(&b, "Secondary muscles: %s\n", join(e.Secondary))
	}

	if e.Instructions != "" {
		fmt.Fprintf(&b, "How to perform it: %s\n", e.Instructions)
	} else {
		// Said out loud so the model reports the gap rather than filling it.
		// 269 of 455 rows arrived with artwork and no text, and invented form
		// advice is the one failure here with physical consequences.
		b.WriteString("How to perform it: not recorded in the catalogue. Say so rather than inventing cues.\n")
	}

	// The two things this tool held and never handed over.
	//
	// Both are public URLs — assets mount outside RequireAuth — so they are
	// safe to give an agent holding no session, and they are what makes an
	// answer showable rather than merely readable. Telegram auto-links a bare
	// URL and an MCP client gets something it can open.
	if e.VideoURL != "" {
		fmt.Fprintf(&b, "Video: %s\n", e.VideoURL)
	}
	if e.HasIllustration() && siteURL != "" {
		fmt.Fprintf(&b, "Illustration: %s/assets/exercises/%s/frame-1.svg\n",
			strings.TrimRight(siteURL, "/"), e.IllustrationSlug)
	}

	return b.String()
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
			Description: "Look up foods in Khepri's ingredient database and read their nutrition per 100g. " +
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
	type args struct {
		Plan string `json:"plan"`
	}

	return Capability{
		Tool: ai.Tool{
			Name: "get_workout_plan",
			Description: "Read a training plan: the days, the focus and start time of each, and every exercise with its sets, reps, rest and load. " +
				"Use it before advising on training, so the advice fits the plan they are actually following, and before editing one.",
			Parameters: ai.Object("which plan", map[string]*ai.Schema{
				"plan": ai.String("a saved plan's name; an empty string for the plan they follow"),
			}, "plan"),
		},
		ReadOnly: true,
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}

			var stored workouts.StoredPlan
			if strings.TrimSpace(in.Plan) != "" {
				plans, listErr := svc.ListCurrentPlans(ctx, userID, 50)
				if listErr != nil {
					return "", listErr
				}
				if stored, err = pickPlan(plans, in.Plan); err != nil {
					return "", err
				}
			} else if stored, err = svc.ActivePlan(ctx, userID); err != nil {
				if apperr.Is(err, apperr.ErrNotFound) {
					// Not an error the model should apologise for. They simply
					// have no plan yet, and saying so lets it offer to build one.
					return "", nil
				}
				return "", err
			}

			return describePlanForEditing(stored.Plan), nil
		},
	}
}

// describePlanForEditing writes a plan with every number an edit can change,
// so the model can see what "everything on two sets" currently covers before
// it asks to change it.
func describePlanForEditing(p workouts.Plan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%d weeks)\n", p.Name, p.WeeksTotal)
	for _, day := range p.Days {
		fmt.Fprintf(&b, "- %s — %s", day.Weekday, day.Focus)
		if day.StartTime != "" {
			fmt.Fprintf(&b, ", at %s", day.StartTime)
		}
		b.WriteString(":\n")
		for _, ex := range day.Exercises {
			fmt.Fprintf(&b, "  - %s %dx%s, %ds rest", ex.Name, ex.Sets, ex.Reps, ex.RestSeconds)
			if ex.Load != "" {
				fmt.Fprintf(&b, " @ %s", ex.Load)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
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
