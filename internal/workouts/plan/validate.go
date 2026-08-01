package plan

import (
	"fmt"
	"strings"
)

// Validate checks a generated plan against what the person actually said.
//
// The schema guarantees shape, not sense. A model can return a perfectly formed
// plan with four training days for someone who said three, or barbell squats
// for someone who owns a pair of dumbbells. Both are unusable on day one, and
// both are exactly the kind of confident wrongness that destroys trust in a
// coach.
//
// Failures are returned as text rather than a sentinel because they are fed
// back to the model on the retry: telling it which rule it broke produces a
// correct plan far more often than asking again.
func Validate(plan Plan, intake Intake) []string {
	var problems []string

	if strings.TrimSpace(plan.Name) == "" {
		problems = append(problems, "the plan has no name")
	}
	if strings.TrimSpace(plan.Rationale) == "" {
		problems = append(problems, "the plan has no rationale")
	}

	if len(plan.Days) != intake.DaysPerWeek {
		problems = append(problems, fmt.Sprintf(
			"the plan has %d training days but they can train exactly %d days a week",
			len(plan.Days), intake.DaysPerWeek))
	}

	available := availableEquipment(intake.Equipment)

	for _, day := range plan.Days {
		if len(day.Exercises) == 0 {
			problems = append(problems, fmt.Sprintf("%s has no exercises", dayLabel(day)))
			continue
		}

		for _, ex := range day.Exercises {
			if strings.TrimSpace(ex.Name) == "" {
				problems = append(problems, fmt.Sprintf("%s contains an exercise with no name", dayLabel(day)))
				continue
			}
			if ex.Sets <= 0 {
				problems = append(problems, fmt.Sprintf("%q has %d sets", ex.Name, ex.Sets))
			}
			if strings.TrimSpace(ex.Reps) == "" {
				problems = append(problems, fmt.Sprintf("%q has no rep range", ex.Name))
			}

			if unavailable, ok := usesUnavailableEquipment(ex, available); !ok {
				problems = append(problems, fmt.Sprintf(
					"%q needs %s, which they do not have (they have: %s)",
					ex.Name, unavailable, strings.Join(intake.Equipment, ", ")))
			}
		}
	}

	return problems
}

// equipmentRule maps a piece of equipment to the words that imply it.
//
// A slice rather than a map, so the order of checks is fixed. With a map, which
// violation gets reported would change between runs, and an error message that
// varies for identical input is a bad error message.
//
// Matching on the exercise name as well as the model's own equipment field
// matters: asked for dumbbell-only training, a model will label a barbell squat
// as needing "free weights" and consider the constraint met. The name is the
// more honest signal.
type equipmentRule struct {
	name     string
	keywords []string
}

// Ordered most specific first. "Barbell bench press" must be caught as a
// barbell before "bench" claims it.
var equipmentRules = []equipmentRule{
	{"barbell", []string{"barbell", "bench press", "deadlift", "back squat", "front squat", "overhead press", "power clean", "rack pull"}},
	{"machine", []string{"machine", "leg press", "lat pulldown", "cable", "smith", "pec deck", "hack squat", "leg extension", "leg curl"}},
	{"dumbbell", []string{"dumbbell", "db "}},
	{"kettlebell", []string{"kettlebell", "kb ", "turkish get"}},
	{"pull-up bar", []string{"pull-up", "pull up", "pullup", "chin-up", "chin up", "chinup", "hanging leg raise"}},
	{"resistance band", []string{"resistance band", "band"}},
	{"rower", []string{"row machine", "rowing machine", "erg"}},
	{"treadmill", []string{"treadmill"}},
	{"bike", []string{"bike", "cycling", "assault bike"}},
	{"bench", []string{"bench"}},
}

// bodyweightOnly are movements that need nothing at all.
//
// Checked only after every equipment rule has been ruled out. Checking them
// first would clear "dumbbell walking lunge" on the word "lunge" and let a plan
// through that the person cannot perform.
var bodyweightOnly = []string{
	"push-up", "push up", "pushup", "plank", "squat jump", "burpee", "lunge",
	"mountain climber", "sit-up", "sit up", "crunch", "glute bridge",
	"bodyweight", "air squat", "run", "walk", "jog", "sprint", "stretch",
}

func availableEquipment(equipment []string) map[string]bool {
	available := make(map[string]bool, len(equipment))
	for _, item := range equipment {
		available[strings.ToLower(strings.TrimSpace(item))] = true
	}
	return available
}

// usesUnavailableEquipment reports the equipment an exercise implies that the
// person does not have.
func usesUnavailableEquipment(ex Exercise, available map[string]bool) (string, bool) {
	haystack := strings.ToLower(ex.Name + " " + ex.Equipment)

	// Equipment wins over the bodyweight list, so a loaded variant of a
	// bodyweight movement is still checked against what they own.
	for _, rule := range equipmentRules {
		for _, keyword := range rule.keywords {
			if !strings.Contains(haystack, keyword) {
				continue
			}
			if available[rule.name] {
				return "", true // named equipment they have; nothing further to check
			}
			return rule.name, false
		}
	}

	for _, phrase := range bodyweightOnly {
		if strings.Contains(haystack, phrase) {
			return "", true
		}
	}

	// Nothing recognised. Assumed fine: guessing that an unknown exercise needs
	// unavailable equipment would reject correct plans, and the prompt already
	// states the constraint.
	return "", true
}

func dayLabel(day PlanDay) string {
	if label := strings.TrimSpace(day.Weekday); label != "" {
		return label
	}
	return "one of the training days"
}
