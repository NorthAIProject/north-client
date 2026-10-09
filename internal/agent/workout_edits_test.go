package agent

import (
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

func editablePlanFixture() plan.Plan {
	return plan.Plan{
		Name:       "Four weeks",
		WeeksTotal: 4,
		Days: []plan.PlanDay{
			{
				Weekday: "Monday", Focus: "lower body",
				Exercises: []plan.Exercise{
					{Name: "Barbell Back Squat", Sets: 5, Reps: "5"},
					{Name: "Romanian Deadlift", Sets: 3, Reps: "8-10"},
				},
			},
			{
				Weekday: "Thursday", Focus: "upper body",
				Exercises: []plan.Exercise{
					{Name: "One-Arm Dumbbell Row", Sets: 3, Reps: "8-12"},
					{Name: "Dumbbell Bench Press", Sets: 3, Reps: "8-12"},
				},
			},
		},
	}
}

func TestPickDayAcceptsAWholeNameOrItsBeginning(t *testing.T) {
	t.Parallel()

	for _, named := range []string{"Monday", "monday", "MONDAY", "Mon"} {
		got, err := pickDay(editablePlanFixture(), named)
		if err != nil {
			t.Errorf("%q: %v", named, err)
			continue
		}
		if got != 0 {
			t.Errorf("%q resolved to day %d, want 0", named, got)
		}
	}
}

// The error has to say what the plan does contain. A model told only "no such
// day" asks the person; a model told the days can correct itself.
func TestPickDayNamesThePlansDaysWhenItCannotResolve(t *testing.T) {
	t.Parallel()

	_, err := pickDay(editablePlanFixture(), "Wednesday")
	if err == nil {
		t.Fatal("a day the plan does not have resolved anyway")
	}
	for _, want := range []string{"Monday", "Thursday"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q: %v", want, err)
		}
	}
}

func TestPickDayRequiresADay(t *testing.T) {
	t.Parallel()

	if _, err := pickDay(editablePlanFixture(), "   "); err == nil {
		t.Error("an empty day resolved to something")
	}
}

// People say "the row", not "One-Arm Dumbbell Row".
func TestPickExerciseMatchesOnPartOfTheName(t *testing.T) {
	t.Parallel()

	day := editablePlanFixture().Days[1]

	got, err := pickExercise(day, "row")
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if got != 0 {
		t.Errorf("resolved to %d, want the row at 0", got)
	}
}

// Guessing between two matches edits the wrong exercise silently, so an
// ambiguous name is an error that lists what it matched.
func TestPickExerciseRefusesToGuessBetweenMatches(t *testing.T) {
	t.Parallel()

	day := editablePlanFixture().Days[1]

	_, err := pickExercise(day, "dumbbell")
	if err == nil {
		t.Fatal("an ambiguous name resolved to one exercise")
	}
	for _, want := range []string{"One-Arm Dumbbell Row", "Dumbbell Bench Press"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not list %q: %v", want, err)
		}
	}
}

func TestPickExerciseNamesTheDaysExercisesWhenNothingMatches(t *testing.T) {
	t.Parallel()

	day := editablePlanFixture().Days[0]

	_, err := pickExercise(day, "bicep curl")
	if err == nil {
		t.Fatal("an exercise the day does not have resolved anyway")
	}
	if !strings.Contains(err.Error(), "Barbell Back Squat") {
		t.Errorf("the error does not say what the day holds: %v", err)
	}
	if !strings.Contains(err.Error(), "Monday") {
		t.Errorf("the error does not say which day: %v", err)
	}
}

func TestPickExerciseRequiresAName(t *testing.T) {
	t.Parallel()

	if _, err := pickExercise(editablePlanFixture().Days[0], ""); err == nil {
		t.Error("an empty name resolved to something")
	}
}

// The safety property the whole design leans on: none of these is ReadOnly, so
// coach.writingCalls holds each one behind an approval card carrying the call
// and its arguments. Marking one read-only would let a model rewrite somebody's
// training plan with no confirmation at all.
//
// Constructed with nil services on purpose — the services are only touched
// inside Invoke, and this is about what the tools declare.
func TestEveryPlanEditIsDeclaredAsAWrite(t *testing.T) {
	t.Parallel()

	capabilities := map[string]Capability{
		"swap_workout_exercise":    swapWorkoutExercise(nil, nil),
		"add_workout_exercise":     addWorkoutExercise(nil, nil),
		"remove_workout_exercise":  removeWorkoutExercise(nil, nil),
		"set_workout_prescription": setWorkoutPrescription(nil, nil),
		"move_workout_exercise":    moveWorkoutExercise(nil, nil),
		"set_workout_start_time":   setWorkoutStartTime(nil, nil),
	}

	for name, c := range capabilities {
		if c.Tool.Name != name {
			t.Errorf("capability is named %q, want %q", c.Tool.Name, name)
		}
		if c.ReadOnly {
			t.Errorf("%s is marked ReadOnly, so it would edit a plan with no approval card", name)
		}
		// Adding twice adds two exercises, and a second swap targets an
		// exercise the first one replaced. Neither is safe to retry blind.
		if c.Idempotent {
			t.Errorf("%s claims to be idempotent, but calling it twice does not leave the same plan", name)
		}
		if c.Tool.Description == "" {
			t.Errorf("%s has no description, so a model has nothing to decide from", name)
		}
	}
}

// A swap and an add both need a catalog slug, and the only place a model can
// get one is search_exercises. Saying so in the description is what stops it
// inventing one.
func TestSlugArgumentsPointAtSearchExercises(t *testing.T) {
	t.Parallel()

	for _, c := range []Capability{swapWorkoutExercise(nil, nil), addWorkoutExercise(nil, nil)} {
		if !strings.Contains(c.Tool.Description, "search_exercises") {
			t.Errorf("%s does not tell the model where slugs come from", c.Tool.Name)
		}
	}
}

// The request behind set_workout_prescription: "everything from 2 sets to 3".
// With no day and no exercise the filter covers the whole plan, and
// only_if_sets narrows it to the exercises on that count.
func TestPrescriptionMatchCoversThePlanWhenNothingIsNamed(t *testing.T) {
	t.Parallel()

	p := editablePlanFixture()
	three := 3
	match, err := prescriptionMatch(p, "", "", &three)
	if err != nil {
		t.Fatalf("match: %v", err)
	}

	var hit []string
	for d, day := range p.Days {
		for _, ex := range day.Exercises {
			if match(d, ex) {
				hit = append(hit, ex.Name)
			}
		}
	}
	want := "Romanian Deadlift, One-Arm Dumbbell Row, Dumbbell Bench Press"
	if got := strings.Join(hit, ", "); got != want {
		t.Errorf("matched %s, want %s", got, want)
	}
}

func TestPrescriptionMatchNarrowsByDayFocusAndName(t *testing.T) {
	t.Parallel()

	p := editablePlanFixture()
	cases := []struct {
		day, exercise string
		want          string
	}{
		{"Monday", "", "Barbell Back Squat, Romanian Deadlift"},
		{"upper body", "", "One-Arm Dumbbell Row, Dumbbell Bench Press"},
		{"", "dumbbell", "One-Arm Dumbbell Row, Dumbbell Bench Press"},
		{"thu", "row", "One-Arm Dumbbell Row"},
	}
	for _, c := range cases {
		match, err := prescriptionMatch(p, c.day, c.exercise, nil)
		if err != nil {
			t.Errorf("%q/%q: %v", c.day, c.exercise, err)
			continue
		}
		var hit []string
		for d, day := range p.Days {
			for _, ex := range day.Exercises {
				if match(d, ex) {
					hit = append(hit, ex.Name)
				}
			}
		}
		if got := strings.Join(hit, ", "); got != c.want {
			t.Errorf("%q/%q matched %s, want %s", c.day, c.exercise, got, c.want)
		}
	}
}

// A name that matches nothing has to say what is there, so the model can
// correct itself instead of asking.
func TestPrescriptionMatchNamesTheExercisesWhenNothingMatches(t *testing.T) {
	t.Parallel()

	_, err := prescriptionMatch(editablePlanFixture(), "Monday", "curl", nil)
	if err == nil {
		t.Fatal("a name matching nothing was accepted")
	}
	if !strings.Contains(err.Error(), "Barbell Back Squat") {
		t.Errorf("the error does not list the day's exercises: %v", err)
	}
	if _, err := prescriptionMatch(editablePlanFixture(), "Sunday", "", nil); err == nil {
		t.Error("a day the plan does not have was accepted")
	}
}
