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

	stored, err := svc.ActivePlan(ctx, userID)
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

// loadNamedPlan is loadEditablePlan for a tool that may name one of the
// person's other saved plans. An empty name means the plan they follow.
func loadNamedPlan(ctx context.Context, svc *workouts.Service, userSvc *users.Service, userID uuid.UUID, name string) (editablePlan, error) {
	if strings.TrimSpace(name) == "" {
		return loadEditablePlan(ctx, svc, userSvc, userID)
	}

	user, err := userSvc.ByID(ctx, userID)
	if err != nil {
		return editablePlan{}, err
	}
	plans, err := svc.ListCurrentPlans(ctx, userID, 50)
	if err != nil {
		return editablePlan{}, err
	}
	chosen, err := pickPlan(plans, name)
	if err != nil {
		return editablePlan{}, err
	}
	return editablePlan{stored: chosen, user: user}, nil
}

// pickDays resolves a day the way pickDay does, but also by the session's
// focus — "Lower B" is how people with a flexible week name a session — and
// returns every day when none is named.
func pickDays(p plan.Plan, day string) ([]int, error) {
	needle := strings.TrimSpace(strings.ToLower(day))
	if needle == "" {
		all := make([]int, len(p.Days))
		for i := range p.Days {
			all[i] = i
		}
		return all, nil
	}

	for i, d := range p.Days {
		if strings.ToLower(d.Focus) == needle {
			return []int{i}, nil
		}
	}
	i, err := pickDay(p, day)
	if err != nil {
		return nil, err
	}
	return []int{i}, nil
}

// prescriptionMatch turns what the model asked for into the filter
// ApplyPrescription runs: which days, which exercises by name, and optionally
// only those currently on a given number of sets.
//
// An exercise name here matches every exercise containing it, on purpose —
// "all the curls to 12 reps" is one request. A name that matches nothing on
// the chosen days is an error naming what is there.
func prescriptionMatch(p plan.Plan, day, exercise string, onlyIfSets *int) (func(int, plan.Exercise) bool, error) {
	days, err := pickDays(p, day)
	if err != nil {
		return nil, err
	}
	inDays := make(map[int]bool, len(days))
	for _, d := range days {
		inDays[d] = true
	}

	needle := strings.TrimSpace(strings.ToLower(exercise))
	if needle != "" {
		found := false
		var names []string
		for _, d := range days {
			for _, ex := range p.Days[d].Exercises {
				names = append(names, ex.Name)
				if strings.Contains(strings.ToLower(ex.Name), needle) {
					found = true
				}
			}
		}
		if !found {
			return nil, fmt.Errorf("no exercise matches %q; the plan has %s", exercise, strings.Join(names, ", "))
		}
	}

	return func(d int, ex plan.Exercise) bool {
		if !inDays[d] {
			return false
		}
		if needle != "" && !strings.Contains(strings.ToLower(ex.Name), needle) {
			return false
		}
		return onlyIfSets == nil || ex.Sets == *onlyIfSets
	}, nil
}

func setWorkoutPrescription(svc *workouts.Service, userSvc *users.Service) Capability {
	type args struct {
		Plan        string  `json:"plan"`
		Day         string  `json:"day"`
		Exercise    string  `json:"exercise"`
		OnlyIfSets  *int    `json:"only_if_sets"`
		Sets        *int    `json:"sets"`
		AddSets     int     `json:"add_sets"`
		Reps        *string `json:"reps"`
		RestSeconds *int    `json:"rest_seconds"`
		Load        *string `json:"load"`
	}

	return Capability{
		Tool: ai.Tool{
			Name: "set_workout_prescription",
			Description: "Change how much of the work to do — sets, reps, rest or load — for one exercise, one day, or the whole training plan, in a single edit. " +
				"Leave day empty for every day and exercise empty for every exercise. " +
				"'Everything from 2 sets to 3' is one call: sets=3, only_if_sets=2. 'One more set on all the squats' is exercise='squat', add_sets=1. " +
				"Never call it once per exercise when one call with a wider filter does the job. The movements stay; only the dose changes.",
			Parameters: ai.Object("which exercises to change, and to what; give only the fields that change, at least one of sets, add_sets, reps, rest_seconds or load", map[string]*ai.Schema{
				"plan":         ai.String("a saved plan's name; leave empty for the plan they follow"),
				"day":          ai.String("a training day such as 'Monday' or a session such as 'Lower B'; an empty string for every day"),
				"exercise":     ai.String("part of an exercise name; every exercise containing it changes. Empty for all"),
				"only_if_sets": ai.Integer("only change exercises currently on this many sets"),
				"sets":         ai.Integer("the new number of sets"),
				"add_sets":     ai.Integer("sets to add (negative to remove); not together with sets"),
				"reps":         ai.String("the new rep range, such as '8-12', '5' or 'AMRAP'"),
				"rest_seconds": ai.Integer("the new rest between sets, in seconds"),
				"load":         ai.String("the target weight as written, such as '100 kg' or 'RPE 8'; an empty string clears it, so omit it to keep it"),
			}, "day"),
		},
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}

			target, err := loadNamedPlan(ctx, svc, userSvc, userID, in.Plan)
			if err != nil {
				return "", err
			}
			// Zero sets is never a valid target, so a provider that fills an
			// omitted integer with 0 means "leave it", not "refuse".
			if in.Sets != nil && *in.Sets == 0 {
				in.Sets = nil
			}
			if in.OnlyIfSets != nil && *in.OnlyIfSets == 0 {
				in.OnlyIfSets = nil
			}
			match, err := prescriptionMatch(target.stored.Plan, in.Day, in.Exercise, in.OnlyIfSets)
			if err != nil {
				return "", err
			}

			_, changed, err := svc.EditPrescriptions(ctx, target.user, target.stored.ID, match, workouts.PrescriptionChange{
				Sets:        in.Sets,
				AddSets:     in.AddSets,
				Reps:        in.Reps,
				RestSeconds: in.RestSeconds,
				Load:        in.Load,
			})
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Updated %d exercises in %s:\n- %s",
				len(changed), target.stored.Plan.Name, strings.Join(changed, "\n- ")), nil
		},
	}
}

func moveWorkoutExercise(svc *workouts.Service, userSvc *users.Service) Capability {
	type args struct {
		Day      string `json:"day"`
		Exercise string `json:"exercise"`
		Position int    `json:"position"`
	}

	return Capability{
		Tool: ai.Tool{
			Name:        "move_workout_exercise",
			Description: "Reorder an exercise within its training day, for example to do a lift first while fresh.",
			Parameters: ai.Object("which exercise to move, and where", map[string]*ai.Schema{
				"day":      ai.String("the training day, such as 'Monday'"),
				"exercise": ai.String("the exercise to move, as it is named in the plan"),
				"position": ai.Integer("its new position in the day, 1 for first"),
			}, "day", "exercise", "position"),
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

			edited, err := svc.MoveExercise(ctx, target.user, target.stored.ID, dayIndex, index, in.Position-1)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Moved %s. %s is now: %s.", day.Exercises[index].Name, day.Weekday,
				strings.Join(exerciseNames(edited.Plan.Days[dayIndex]), ", ")), nil
		},
	}
}

func setWorkoutStartTime(svc *workouts.Service, userSvc *users.Service) Capability {
	type args struct {
		Days      []string `json:"days"`
		StartTime string   `json:"start_time"`
	}

	return Capability{
		Tool: ai.Tool{
			Name:        "set_workout_start_time",
			Description: "Set or clear when training sessions start. Leave days empty to set every training day at once.",
			Parameters: ai.Object("which days, and when", map[string]*ai.Schema{
				"days":       ai.Array("training days such as 'Monday'; empty for every day", ai.String("a training day")),
				"start_time": ai.String("HH:MM on a 24-hour clock, or empty to clear"),
			}, "start_time"),
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

			var days []int
			if len(in.Days) == 0 {
				days, _ = pickDays(target.stored.Plan, "")
			}
			for _, name := range in.Days {
				i, err := pickDay(target.stored.Plan, name)
				if err != nil {
					return "", err
				}
				days = append(days, i)
			}

			if _, err := svc.SetStartTimes(ctx, target.user, target.stored.ID, days, in.StartTime); err != nil {
				return "", err
			}
			names := make([]string, 0, len(days))
			for _, i := range days {
				names = append(names, target.stored.Plan.Days[i].Weekday)
			}
			if strings.TrimSpace(in.StartTime) == "" {
				return "Cleared the start time on " + strings.Join(names, ", ") + ".", nil
			}
			return fmt.Sprintf("%s now start at %s.", strings.Join(names, ", "), strings.TrimSpace(in.StartTime)), nil
		},
	}
}
