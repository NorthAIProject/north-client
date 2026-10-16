package plan

import (
	"strings"
	"testing"
)

func TestEstimateMinutesCountsWarmUpWorkAndRest(t *testing.T) {
	t.Parallel()

	// 5 min warm-up + 3 × (45 s + 90 s) + 4 × (45 s + 60 s) = 300 + 405 + 420 s = 18.75 → 19.
	d := day("Monday", exercise("Squat", "none"), Exercise{Name: "Plank", Sets: 4, Reps: "30s", RestSeconds: 60})
	if got := EstimateMinutes(d); got != 19 {
		t.Fatalf("EstimateMinutes = %d, want 19", got)
	}
	// Unstated rest is the default, not zero: nobody does sets back to back.
	if got := EstimateMinutes(day("Monday", Exercise{Name: "Row", Sets: 4, Reps: "10"})); got != 5+9 {
		t.Fatalf("EstimateMinutes with no rest stated = %d, want 14", got)
	}
}

func TestValidateRejectsASessionLongerThanTheyHave(t *testing.T) {
	t.Parallel()

	long := make([]Exercise, 8)
	for i := range long {
		long[i] = exercise("Push-up", "none") // 8 × 3 × 135 s + 5 min = 59 min
	}
	in := dumbbellOnly() // 45 minutes, 15% grace is 51.75
	p := planWith(day("Monday", long...), day("Wednesday", exercise("Push-up", "none")), day("Friday", exercise("Push-up", "none")))

	problems := strings.Join(Validate(p, in), "\n")
	if !strings.Contains(problems, "Monday takes about 59 minutes with warm-up and rest, but they have 45") {
		t.Fatalf("problems = %q", problems)
	}

	in.SessionMinutes = 55 // 59 is inside 15% of 55
	if problems := Validate(p, in); len(problems) != 0 {
		t.Fatalf("a session within the grace was refused: %q", problems)
	}
}

func TestValidateHoldsThePlanToTheWeekdaysTheyChose(t *testing.T) {
	t.Parallel()

	in := dumbbellOnly()
	in.Weekdays = []string{"Tuesday", "Thursday", "Saturday"}

	fits := planWith(day("saturday", exercise("Push-up", "none")), day("Tuesday", exercise("Push-up", "none")), day("Thursday", exercise("Push-up", "none")))
	if problems := Validate(fits, in); len(problems) != 0 {
		t.Fatalf("the chosen days in another order and case were refused: %q", problems)
	}

	wrong := planWith(day("Monday", exercise("Push-up", "none")), day("Wednesday", exercise("Push-up", "none")), day("Friday", exercise("Push-up", "none")))
	problems := strings.Join(Validate(wrong, in), "\n")
	if !strings.Contains(problems, "the plan trains on Monday, Wednesday, Friday but they can only train on Tuesday, Thursday, Saturday") {
		t.Fatalf("problems = %q", problems)
	}

	in.Weekdays = nil
	if problems := Validate(wrong, in); len(problems) != 0 {
		t.Fatalf("with no days chosen any weekdays do: %q", problems)
	}
}
