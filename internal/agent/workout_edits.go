package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

// Editing a training plan from the chat.
//
// The same operations the plan page offers, reached by talking instead of
// clicking — "swap the barbell row for something with dumbbells" is a sentence
// people say, and it is a worse experience to answer it with directions to a
// button.
//
// Three things shape these tools:
//
// None of them take a plan id. The model works on whatever plan the person is
// currently following, resolved here, for the same reason get_workout_plan
// does: a plan id is not something anyone says out loud, and one arriving in
// arguments would be a value the model had invented.
//
// Positions are named, not numbered. A day is "Monday" and an exercise is the
// name it is written under, resolved the way pickGoal resolves a goal title —
// ambiguity is an error listing the choices rather than a guess, because
// guessing edits the wrong exercise silently.
//
// None are ReadOnly, so every one of them shows the person an approval card
// carrying the call and its arguments before it runs (see coach.writingCalls).
// That is what makes editing by conversation safe while there is still no way
// to see or undo an edit in the interface.

// editableDayAndExercise resolves what the model named against the plan the
// person is actually following.
type editablePlan struct {
	stored workouts.StoredPlan
	user   users.User
}

func loadEditablePlan(ctx context.Context, svc *workouts.Service, userSvc *users.Service, userID uuid.UUID) (editablePlan, error) {
	// The whole record: the edit methods take a users.User, because they are
	// the same ones the web handler calls.
	user, err := userSvc.ByID(ctx, userID)
	if err != nil {
		return editablePlan{}, err
	}

	stored, err := svc.LatestPlan(ctx, userID)
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			// Not something to apologise for. They have no plan, and saying so
			// lets the model offer to build one.
			return editablePlan{}, fmt.Errorf("this person has no training plan yet")
		}
		return editablePlan{}, err
	}

	return editablePlan{stored: stored, user: user}, nil
}

// pickDay resolves a weekday to its position in the plan.
//
// A plan names its own days, and two of them can share a weekday only if the
// generator produced something odd — so an exact match is expected and a
// prefix is enough for "Mon".
func pickDay(p plan.Plan, weekday string) (int, error) {
	needle := strings.TrimSpace(strings.ToLower(weekday))
	if needle == "" {
		return 0, fmt.Errorf("name the day to change")
	}

	var hits []int
	for i, day := range p.Days {
		if strings.HasPrefix(strings.ToLower(day.Weekday), needle) {
			hits = append(hits, i)
		}
	}

	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return 0, fmt.Errorf("this plan has no %s; its days are %s", weekday, strings.Join(weekdays(p), ", "))
	default:
		return 0, fmt.Errorf("%q matches more than one day; its days are %s", weekday, strings.Join(weekdays(p), ", "))
	}
}

// pickExercise resolves an exercise name to its position within a day.
//
// Substring rather than exact, because a person says "the row" for "One-Arm
// Dumbbell Row". Ambiguity is an error listing what matched, for the same
// reason it is in pickGoal: editing the wrong exercise is silent, and the model
// can ask once it knows the choices.
func pickExercise(day plan.PlanDay, name string) (int, error) {
	needle := strings.TrimSpace(strings.ToLower(name))
	if needle == "" {
		return 0, fmt.Errorf("name the exercise to change")
	}

	var hits []int
	for i, ex := range day.Exercises {
		if strings.Contains(strings.ToLower(ex.Name), needle) {
			hits = append(hits, i)
		}
	}

	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return 0, fmt.Errorf("nothing on %s matches %q; it has %s", day.Weekday, name, strings.Join(exerciseNames(day), ", "))
	default:
		matched := make([]string, 0, len(hits))
		for _, i := range hits {
			matched = append(matched, day.Exercises[i].Name)
		}
		return 0, fmt.Errorf("%q matches several exercises on %s (%s); be more specific", name, day.Weekday, strings.Join(matched, ", "))
	}
}

func weekdays(p plan.Plan) []string {
	names := make([]string, 0, len(p.Days))
	for _, day := range p.Days {
		names = append(names, day.Weekday)
	}
	return names
}

func exerciseNames(day plan.PlanDay) []string {
	names := make([]string, 0, len(day.Exercises))
	for _, ex := range day.Exercises {
		names = append(names, ex.Name)
	}
	return names
}

func swapWorkoutExercise(svc *workouts.Service, userSvc *users.Service) Capability {
	type args struct {
		Day      string `json:"day"`
		Exercise string `json:"exercise"`
		Slug     string `json:"slug"`
	}

	return Capability{
		Tool: ai.Tool{
			Name: "swap_workout_exercise",
			Description: "Replace one exercise in this person's training plan with a different one, keeping its sets, reps and rest. " +
				"Use search_exercises first to find the replacement's slug. " +
				"Good for when equipment is unavailable, something aggravates an injury, or they simply dislike a movement.",
			Parameters: ai.Object("which exercise to replace, and with what", map[string]*ai.Schema{
				"day":      ai.String("the training day, such as 'Monday'"),
				"exercise": ai.String("the exercise to replace, as it is named in the plan"),
				"slug":     ai.String("the replacement's slug, as returned by search_exercises"),
			}, "day", "exercise", "slug"),
		},
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}

			target, err := loadEditablePlan(ctx, svc, userSvc, userID)
			if err != nil {
				return "", err
			}

			dayIndex, err := pickDay(target.stored.Plan, in.Day)
			if err != nil {
				return "", err
			}
			day := target.stored.Plan.Days[dayIndex]

			index, err := pickExercise(day, in.Exercise)
			if err != nil {
				return "", err
			}
			replaced := day.Exercises[index].Name

			edited, err := svc.SwapExercise(ctx, target.user, target.stored.ID, dayIndex, index, in.Slug)
			if err != nil {
				return "", err
			}

			now := edited.Plan.Days[dayIndex].Exercises[index]
			return fmt.Sprintf("Swapped %s for %s on %s, still %d×%s.",
				replaced, now.Name, day.Weekday, now.Sets, now.Reps), nil
		},
	}
}

func addWorkoutExercise(svc *workouts.Service, userSvc *users.Service) Capability {
	type args struct {
		Day  string `json:"day"`
		Slug string `json:"slug"`
	}

	return Capability{
		Tool: ai.Tool{
			Name: "add_workout_exercise",
			Description: fmt.Sprintf(
				"Add an exercise to the end of a training day. Use search_exercises first to find its slug. "+
					"It starts at %d×%s with %ds rest, which the person can change afterwards.",
				plan.DefaultSets, plan.DefaultReps, plan.DefaultRestSeconds),
			Parameters: ai.Object("what to add, and where", map[string]*ai.Schema{
				"day":  ai.String("the training day, such as 'Monday'"),
				"slug": ai.String("the exercise's slug, as returned by search_exercises"),
			}, "day", "slug"),
		},
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}

			target, err := loadEditablePlan(ctx, svc, userSvc, userID)
			if err != nil {
				return "", err
			}

			dayIndex, err := pickDay(target.stored.Plan, in.Day)
			if err != nil {
				return "", err
			}
			weekday := target.stored.Plan.Days[dayIndex].Weekday

			edited, err := svc.AddExercise(ctx, target.user, target.stored.ID, dayIndex, in.Slug)
			if err != nil {
				return "", err
			}

			added := edited.Plan.Days[dayIndex].Exercises
			last := added[len(added)-1]
			return fmt.Sprintf("Added %s to %s at %d×%s, %ds rest.",
				last.Name, weekday, last.Sets, last.Reps, last.RestSeconds), nil
		},
	}
}

func removeWorkoutExercise(svc *workouts.Service, userSvc *users.Service) Capability {
	type args struct {
		Day      string `json:"day"`
		Exercise string `json:"exercise"`
	}

	return Capability{
		Tool: ai.Tool{
			Name: "remove_workout_exercise",
			Description: "Take an exercise off a training day. " +
				"The previous version of the plan is kept, so this is recoverable, but prefer swapping when they want the work done differently rather than not at all.",
			Parameters: ai.Object("which exercise to remove", map[string]*ai.Schema{
				"day":      ai.String("the training day, such as 'Monday'"),
				"exercise": ai.String("the exercise to remove, as it is named in the plan"),
			}, "day", "exercise"),
		},
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}

			target, err := loadEditablePlan(ctx, svc, userSvc, userID)
			if err != nil {
				return "", err
			}

			dayIndex, err := pickDay(target.stored.Plan, in.Day)
			if err != nil {
				return "", err
			}
			day := target.stored.Plan.Days[dayIndex]

			index, err := pickExercise(day, in.Exercise)
			if err != nil {
				return "", err
			}
			removed := day.Exercises[index].Name

			edited, err := svc.RemoveExercise(ctx, target.user, target.stored.ID, dayIndex, index)
			if err != nil {
				return "", err
			}

			left := len(edited.Plan.Days[dayIndex].Exercises)
			if left == 0 {
				return fmt.Sprintf("Removed %s. %s now has nothing on it.", removed, day.Weekday), nil
			}
			return fmt.Sprintf("Removed %s from %s, leaving %d exercises.", removed, day.Weekday, left), nil
		},
	}
}
