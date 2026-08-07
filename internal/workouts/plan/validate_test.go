package plan

import (
	"strings"
	"testing"
)

func dumbbellOnly() Intake {
	return Intake{
		Goal:           "general strength",
		Experience:     "beginner",
		DaysPerWeek:    3,
		SessionMinutes: 45,
		Equipment:      []string{"dumbbell"},
	}
}

func planWith(days ...PlanDay) Plan {
	return Plan{Name: "Test plan", Rationale: "Because.", WeeksTotal: 8, Days: days}
}

func day(weekday string, exercises ...Exercise) PlanDay {
	return PlanDay{Weekday: weekday, Focus: "full body", Exercises: exercises}
}

func exercise(name, equipment string) Exercise {
	return Exercise{Name: name, Sets: 3, Reps: "8-12", RestSeconds: 90, Equipment: equipment}
}

func TestValidateAcceptsAConformingPlan(t *testing.T) {
	t.Parallel()

	plan := planWith(
		day("Monday", exercise("Dumbbell Goblet Squat", "dumbbell")),
		day("Wednesday", exercise("Push-up", "none")),
		day("Friday", exercise("Dumbbell Row", "dumbbell")),
	)

	if problems := Validate(plan, dumbbellOnly()); len(problems) != 0 {
		t.Fatalf("a conforming plan was rejected: %v", problems)
	}
}

func TestValidateRejectsWrongNumberOfDays(t *testing.T) {
	t.Parallel()

	plan := planWith(
		day("Monday", exercise("Push-up", "none")),
		day("Tuesday", exercise("Push-up", "none")),
		day("Thursday", exercise("Push-up", "none")),
		day("Saturday", exercise("Push-up", "none")),
	)

	problems := Validate(plan, dumbbellOnly())
	if len(problems) == 0 {
		t.Fatal("four days for a three-day intake must be rejected")
	}
	if !strings.Contains(problems[0], "4 training days") || !strings.Contains(problems[0], "3 days") {
		t.Fatalf("the message should name both counts so the retry can fix it: %q", problems[0])
	}
}

func TestValidateRejectsUnavailableEquipment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		exercise Exercise
		wants    string
	}{
		{"barbell lift", exercise("Barbell Back Squat", "barbell"), "barbell"},
		// The model claiming "free weights" must not launder a barbell lift
		// past the check; the exercise name is what gives it away.
		{"barbell disguised in the equipment field", exercise("Deadlift", "free weights"), "barbell"},
		{"machine", exercise("Leg Press", "machine"), "machine"},
		{"cable", exercise("Cable Fly", "cable machine"), "machine"},
		{"pull-up bar", exercise("Pull-up", "bar"), "pull-up bar"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			plan := planWith(
				day("Monday", tt.exercise),
				day("Wednesday", exercise("Push-up", "none")),
				day("Friday", exercise("Push-up", "none")),
			)

			problems := Validate(plan, dumbbellOnly())
			if len(problems) == 0 {
				t.Fatalf("%q should have been rejected", tt.exercise.Name)
			}
			if !strings.Contains(strings.Join(problems, " "), tt.wants) {
				t.Fatalf("expected the failure to name %q, got %v", tt.wants, problems)
			}
		})
	}
}

// The bug this test exists for: checking the bodyweight word list first cleared
// "Dumbbell Walking Lunge" on the word "lunge", letting a plan through that the
// person could not perform.
func TestLoadedVariantsOfBodyweightMovesAreStillChecked(t *testing.T) {
	t.Parallel()

	bodyweightIntake := dumbbellOnly()
	bodyweightIntake.Equipment = nil // no equipment at all

	plan := planWith(
		day("Monday", exercise("Dumbbell Walking Lunge", "dumbbell")),
		day("Wednesday", exercise("Push-up", "none")),
		day("Friday", exercise("Plank", "none")),
	)

	problems := Validate(plan, bodyweightIntake)
	if len(problems) == 0 {
		t.Fatal("a dumbbell lunge must be rejected for someone with no equipment")
	}
	if !strings.Contains(strings.Join(problems, " "), "dumbbell") {
		t.Fatalf("expected the dumbbell to be named: %v", problems)
	}
}

func TestBodyweightIsAlwaysAllowed(t *testing.T) {
	t.Parallel()

	noEquipment := dumbbellOnly()
	noEquipment.Equipment = nil

	plan := planWith(
		day("Monday", exercise("Push-up", "none")),
		day("Wednesday", exercise("Air Squat", "bodyweight")),
		day("Friday", exercise("Plank", "none")),
	)

	if problems := Validate(plan, noEquipment); len(problems) != 0 {
		t.Fatalf("bodyweight training needs no equipment: %v", problems)
	}
}

func TestValidateRejectsStructurallyBrokenExercises(t *testing.T) {
	t.Parallel()

	plan := planWith(
		day("Monday", Exercise{Name: "Push-up", Sets: 0, Reps: "10"}),
		day("Wednesday", Exercise{Name: "Plank", Sets: 3, Reps: ""}),
		day("Friday"),
	)

	problems := Validate(plan, dumbbellOnly())
	joined := strings.Join(problems, " | ")

	for _, want := range []string{"0 sets", "no rep range", "no exercises"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected a failure mentioning %q, got %v", want, problems)
		}
	}
}

func TestValidateRequiresNameAndRationale(t *testing.T) {
	t.Parallel()

	plan := Plan{
		Days: []PlanDay{
			day("Monday", exercise("Push-up", "none")),
			day("Wednesday", exercise("Push-up", "none")),
			day("Friday", exercise("Push-up", "none")),
		},
	}

	problems := Validate(plan, dumbbellOnly())
	joined := strings.Join(problems, " | ")

	// The rationale is how the plan explains itself to the person. A plan
	// without one is a list of exercises, which is what they could have got
	// anywhere.
	if !strings.Contains(joined, "no name") || !strings.Contains(joined, "no rationale") {
		t.Fatalf("expected both to be required: %v", problems)
	}
}

func TestValidateDropsMuscleKeysOutsideTheTaxonomy(t *testing.T) {
	t.Parallel()

	ex := exercise("Push-up", "none")
	// "pecs" is the synonym a model reaches for; the canonical key is "chest".
	ex.Primary = []string{"pecs", "abs"}
	ex.Secondary = []string{"triceps", "shoulders"}
	ex.Stabilizers = []string{"not-a-key"}

	plan := planWith(
		day("Monday", ex),
		day("Wednesday", exercise("Push-up", "none")),
		day("Friday", exercise("Push-up", "none")),
	)

	if problems := Validate(plan, dumbbellOnly()); len(problems) != 0 {
		t.Fatalf("an out-of-taxonomy muscle key should be dropped, not rejected: %v", problems)
	}

	got := plan.Days[0].Exercises[0]
	if want := []string{"abs"}; !equalStrings(got.Primary, want) {
		t.Errorf("Primary = %v, want %v", got.Primary, want)
	}
	if want := []string{"triceps"}; !equalStrings(got.Secondary, want) {
		t.Errorf("Secondary = %v, want %v", got.Secondary, want)
	}
	if len(got.Stabilizers) != 0 {
		t.Errorf("Stabilizers = %v, want empty", got.Stabilizers)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The bug this test exists for: the barbell rule's "deadlift" keyword was
// checked before the dumbbell rule, so "Romanian Deadlift With Dumbbells" was
// judged to need a barbell — and someone holding two dumbbells was told they
// could not do it.
func TestANamedImplementBeatsAMovementNameKeyword(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, equipment, want string }{
		{"Romanian Deadlift With Dumbbells", "dumbbell", "dumbbell"},
		{"Dumbbell Bench Press", "dumbbell", "dumbbell"},
		{"Kettlebell Sumo Deadlift High Pull", "kettlebells", "kettlebell"},
		// Still a barbell when nothing else is named.
		{"Barbell Bench Press - Medium Grip", "barbell", "barbell"},
		{"Deadlift", "free weights", "barbell"},
		{"Sumo Deadlift", "barbell", "barbell"},
		{"Push-up", "body_only", "none"},
	}

	for _, tt := range tests {
		if got := InferEquipment(tt.name, tt.equipment); got != tt.want {
			t.Errorf("InferEquipment(%q, %q) = %q, want %q", tt.name, tt.equipment, got, tt.want)
		}
	}
}

func TestDumbbellVariantsAreAllowedForSomeoneWithDumbbells(t *testing.T) {
	t.Parallel()

	plan := planWith(
		day("Monday", exercise("Romanian Deadlift With Dumbbells", "dumbbell")),
		day("Wednesday", exercise("Dumbbell Bench Press", "dumbbell")),
		day("Friday", exercise("Push-up", "none")),
	)

	if problems := Validate(plan, dumbbellOnly()); len(problems) != 0 {
		t.Fatalf("dumbbell variants must be allowed for someone with dumbbells: %v", problems)
	}
}

func TestEquipmentTheyOwnIsAccepted(t *testing.T) {
	t.Parallel()

	fullGym := Intake{
		Goal: "strength", Experience: "intermediate",
		DaysPerWeek: 1, SessionMinutes: 60,
		Equipment: []string{"barbell", "bench", "machine", "pull-up bar"},
	}

	plan := planWith(day("Monday",
		exercise("Barbell Bench Press", "barbell"),
		exercise("Leg Press", "machine"),
		exercise("Pull-up", "pull-up bar"),
	))

	if problems := Validate(plan, fullGym); len(problems) != 0 {
		t.Fatalf("equipment they own must be accepted: %v", problems)
	}
}
