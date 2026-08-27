package plan

import (
	"strings"
	"testing"
)

// Swap's contract is a training decision, not a data one: the prescription
// survives and the movement does not. These pin it without a database.

func samplePlan() Plan {
	return Plan{
		Name:       "Four weeks",
		WeeksTotal: 4,
		Days: []PlanDay{{
			Weekday: "Monday",
			Focus:   "lower body",
			Exercises: []Exercise{
				{
					Name: "Barbell Back Squat", Sets: 5, Reps: "5", RestSeconds: 180,
					Equipment: "barbell", FormCues: "Knees out.", Substitute: "Goblet squat",
					CatalogSlug: "barbell-full-squat", IllustrationSlug: "squat",
					Primary: []string{"quads"}, Secondary: []string{"glutes"},
					Stabilizers: []string{"abs"},
				},
				{
					Name: "Romanian Deadlift", Sets: 3, Reps: "8-10", RestSeconds: 120,
					Equipment: "barbell", CatalogSlug: "romanian-deadlift",
					Primary: []string{"hamstrings"},
				},
			},
		}},
	}
}

func gobletSquat() Movement {
	return Movement{
		Name: "Dumbbell Goblet Squat", Equipment: "dumbbell",
		CatalogSlug: "dumbbell-goblet-squat", IllustrationSlug: "goblet-squat",
		Primary: []string{"quads"}, Secondary: []string{"glutes", "abs"},
	}
}

// The whole point of a swap: the gym is busy, the work is not negotiable.
func TestSwapKeepsThePrescriptionAndReplacesTheMovement(t *testing.T) {
	t.Parallel()

	out, err := Swap(samplePlan(), 0, 0, gobletSquat())
	if err != nil {
		t.Fatalf("swap: %v", err)
	}

	got := out.Days[0].Exercises[0]
	if got.Sets != 5 || got.Reps != "5" || got.RestSeconds != 180 {
		t.Errorf("prescription changed: %d x %s, %ds rest", got.Sets, got.Reps, got.RestSeconds)
	}
	if got.Substitute != "Goblet squat" {
		t.Errorf("Substitute = %q, want it kept", got.Substitute)
	}
	if got.Name != "Dumbbell Goblet Squat" || got.Equipment != "dumbbell" {
		t.Errorf("movement not replaced: %q on %q", got.Name, got.Equipment)
	}
	if got.CatalogSlug != "dumbbell-goblet-squat" || got.IllustrationSlug != "goblet-squat" {
		t.Errorf("catalog references not replaced: %q / %q", got.CatalogSlug, got.IllustrationSlug)
	}
}

// A cue describing the previous lift is worse than no cue, and the catalog has
// nothing to replace it with. Same for stabilizers, which the model produced
// for a movement that is no longer there.
func TestSwapClearsWhatDescribedTheOldMovement(t *testing.T) {
	t.Parallel()

	out, err := Swap(samplePlan(), 0, 0, gobletSquat())
	if err != nil {
		t.Fatalf("swap: %v", err)
	}

	got := out.Days[0].Exercises[0]
	if got.FormCues != "" {
		t.Errorf("FormCues = %q, want cleared — it described the old lift", got.FormCues)
	}
	if len(got.Stabilizers) != 0 {
		t.Errorf("Stabilizers = %v, want cleared", got.Stabilizers)
	}
}

// The service writes the result as a new row while the original stays exactly
// as it was stored, so a swap must not reach back into the plan it was given.
func TestSwapDoesNotMutateTheOriginalPlan(t *testing.T) {
	t.Parallel()

	original := samplePlan()
	if _, err := Swap(original, 0, 0, gobletSquat()); err != nil {
		t.Fatalf("swap: %v", err)
	}

	if got := original.Days[0].Exercises[0].Name; got != "Barbell Back Squat" {
		t.Errorf("the original plan was mutated: exercise is now %q", got)
	}
}

func TestSwapLeavesEveryOtherExerciseAlone(t *testing.T) {
	t.Parallel()

	out, err := Swap(samplePlan(), 0, 0, gobletSquat())
	if err != nil {
		t.Fatalf("swap: %v", err)
	}

	if got := out.Days[0].Exercises[1]; got.Name != "Romanian Deadlift" || got.Sets != 3 {
		t.Errorf("the neighbouring exercise changed: %+v", got)
	}
}

// The indices come from a URL, so out of range is an ordinary bad request and
// must not panic.
func TestSwapRejectsAPositionOutsideThePlan(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		day, index int
	}{
		{"day past the end", 9, 0},
		{"negative day", -1, 0},
		{"exercise past the end", 0, 9},
		{"negative exercise", 0, -1},
	}

	for _, c := range cases {
		if _, err := Swap(samplePlan(), c.day, c.index, gobletSquat()); err == nil {
			t.Errorf("%s: swap succeeded, want an error", c.name)
		}
	}
}

// The muscle keys are copied rather than aliased: a later edit of the catalog
// row's slice must not reach into a stored plan.
func TestSwapCopiesTheMuscleKeysRatherThanSharingThem(t *testing.T) {
	t.Parallel()

	movement := gobletSquat()
	out, err := Swap(samplePlan(), 0, 0, movement)
	if err != nil {
		t.Fatalf("swap: %v", err)
	}

	movement.Primary[0] = "mutated"
	if got := out.Days[0].Exercises[0].Primary[0]; got != "quads" {
		t.Errorf("the plan shares the caller's slice: primary is now %q", got)
	}
}

func TestInsertAppendsWhenTheIndexIsTheLength(t *testing.T) {
	t.Parallel()

	p := samplePlan()
	out, err := Insert(p, 0, len(p.Days[0].Exercises), NewExercise(gobletSquat()))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	if got := len(out.Days[0].Exercises); got != 3 {
		t.Fatalf("day has %d exercises, want 3", got)
	}
	if got := out.Days[0].Exercises[2].Name; got != "Dumbbell Goblet Squat" {
		t.Errorf("appended exercise is %q", got)
	}
}

func TestInsertShiftsTheRestDown(t *testing.T) {
	t.Parallel()

	out, err := Insert(samplePlan(), 0, 0, NewExercise(gobletSquat()))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	names := []string{out.Days[0].Exercises[0].Name, out.Days[0].Exercises[1].Name, out.Days[0].Exercises[2].Name}
	want := []string{"Dumbbell Goblet Squat", "Barbell Back Squat", "Romanian Deadlift"}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("position %d = %q, want %q", i, names[i], want[i])
		}
	}
}

// A new exercise has no prescription to inherit, so it takes the documented
// defaults rather than something that looks reasoned about.
func TestNewExerciseStartsAtTheDocumentedDefaults(t *testing.T) {
	t.Parallel()

	ex := NewExercise(gobletSquat())
	if ex.Sets != DefaultSets || ex.Reps != DefaultReps || ex.RestSeconds != DefaultRestSeconds {
		t.Errorf("defaults = %d x %s, %ds rest", ex.Sets, ex.Reps, ex.RestSeconds)
	}
	if ex.FormCues != "" {
		t.Errorf("FormCues = %q, want empty — the catalog carries none", ex.FormCues)
	}
	if ex.CatalogSlug != "dumbbell-goblet-squat" {
		t.Errorf("CatalogSlug = %q", ex.CatalogSlug)
	}
}

func TestRemoveDropsOnlyTheNamedExercise(t *testing.T) {
	t.Parallel()

	out, err := Remove(samplePlan(), 0, 0)
	if err != nil {
		t.Fatalf("remove: %v", err)
	}

	if got := len(out.Days[0].Exercises); got != 1 {
		t.Fatalf("day has %d exercises, want 1", got)
	}
	if got := out.Days[0].Exercises[0].Name; got != "Romanian Deadlift" {
		t.Errorf("the wrong exercise survived: %q", got)
	}
}

// Clearing a day out before rebuilding it is reasonable, and refusing the last
// removal would mean the only way to replace everything is add-then-delete.
func TestRemoveAllowsADayToBecomeEmpty(t *testing.T) {
	t.Parallel()

	out, err := Remove(samplePlan(), 0, 0)
	if err != nil {
		t.Fatalf("first remove: %v", err)
	}
	out, err = Remove(out, 0, 0)
	if err != nil {
		t.Fatalf("second remove: %v", err)
	}

	if got := len(out.Days[0].Exercises); got != 0 {
		t.Errorf("day has %d exercises, want an empty day to be allowed", got)
	}
}

// Both operations rebuild the day's slice. Sharing a backing array with the
// plan they were given would let an edit reach back into the row already
// stored, which is the one thing append-only storage exists to prevent.
func TestInsertAndRemoveDoNotDisturbTheOriginalPlan(t *testing.T) {
	t.Parallel()

	original := samplePlan()

	if _, err := Insert(original, 0, 0, NewExercise(gobletSquat())); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := Remove(original, 0, 0); err != nil {
		t.Fatalf("remove: %v", err)
	}

	if got := len(original.Days[0].Exercises); got != 2 {
		t.Fatalf("the original day now has %d exercises, want 2", got)
	}
	if got := original.Days[0].Exercises[0].Name; got != "Barbell Back Squat" {
		t.Errorf("the original's first exercise is now %q", got)
	}
	if got := original.Days[0].Exercises[1].Name; got != "Romanian Deadlift" {
		t.Errorf("the original's second exercise is now %q", got)
	}
}

func TestInsertAndRemoveRejectAPositionOutsideThePlan(t *testing.T) {
	t.Parallel()

	if _, err := Insert(samplePlan(), 0, 99, NewExercise(gobletSquat())); err == nil {
		t.Error("insert past the end succeeded")
	}
	if _, err := Insert(samplePlan(), 9, 0, NewExercise(gobletSquat())); err == nil {
		t.Error("insert into a day that does not exist succeeded")
	}
	if _, err := Remove(samplePlan(), 0, 99); err == nil {
		t.Error("remove past the end succeeded")
	}
	if _, err := Remove(samplePlan(), 0, -1); err == nil {
		t.Error("remove at a negative index succeeded")
	}
}

func namesOf(day PlanDay) []string {
	names := make([]string, 0, len(day.Exercises))
	for _, ex := range day.Exercises {
		names = append(names, ex.Name)
	}
	return names
}

func equalNames(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// A three-exercise day, so a move has somewhere to land that is neither end.
func threeExerciseDay() Plan {
	p := samplePlan()
	p.Days[0].Exercises = append(p.Days[0].Exercises, Exercise{
		Name: "Leg Press", Sets: 3, Reps: "12", RestSeconds: 90,
		Equipment: "machine", CatalogSlug: "leg-press", Primary: []string{"quads"},
	})
	return p
}

func TestMoveReordersWithinTheDay(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		from, to int
		want     []string
	}{
		{"down one", 0, 1, []string{"Romanian Deadlift", "Barbell Back Squat", "Leg Press"}},
		{"up one", 2, 1, []string{"Barbell Back Squat", "Leg Press", "Romanian Deadlift"}},
		{"to the end", 0, 2, []string{"Romanian Deadlift", "Leg Press", "Barbell Back Squat"}},
		{"to the start", 2, 0, []string{"Leg Press", "Barbell Back Squat", "Romanian Deadlift"}},
		{"nowhere", 1, 1, []string{"Barbell Back Squat", "Romanian Deadlift", "Leg Press"}},
	}

	for _, c := range cases {
		out, err := Move(threeExerciseDay(), 0, c.from, c.to)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if got := namesOf(out.Days[0]); !equalNames(got, c.want) {
			t.Errorf("%s: order = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestMoveDoesNotDisturbTheOriginalPlan(t *testing.T) {
	t.Parallel()

	original := threeExerciseDay()
	if _, err := Move(original, 0, 0, 2); err != nil {
		t.Fatalf("move: %v", err)
	}

	want := []string{"Barbell Back Squat", "Romanian Deadlift", "Leg Press"}
	if got := namesOf(original.Days[0]); !equalNames(got, want) {
		t.Errorf("the original was reordered: %v", got)
	}
}

func TestMoveRejectsAPositionOutsideTheDay(t *testing.T) {
	t.Parallel()

	if _, err := Move(threeExerciseDay(), 0, 0, 9); err == nil {
		t.Error("move past the end succeeded")
	}
	if _, err := Move(threeExerciseDay(), 0, 0, -1); err == nil {
		t.Error("move to a negative position succeeded")
	}
	if _, err := Move(threeExerciseDay(), 0, 9, 0); err == nil {
		t.Error("move from past the end succeeded")
	}
}

// SetPrescription is the inverse of Swap: the dose changes, the movement does
// not.
func TestSetPrescriptionChangesOnlyTheDose(t *testing.T) {
	t.Parallel()

	out, err := SetPrescription(samplePlan(), 0, 0, 4, "6-8", 150)
	if err != nil {
		t.Fatalf("set: %v", err)
	}

	got := out.Days[0].Exercises[0]
	if got.Sets != 4 || got.Reps != "6-8" || got.RestSeconds != 150 {
		t.Errorf("prescription = %d x %s, %ds", got.Sets, got.Reps, got.RestSeconds)
	}
	if got.Name != "Barbell Back Squat" || got.CatalogSlug != "barbell-full-squat" {
		t.Errorf("the movement changed: %q / %q", got.Name, got.CatalogSlug)
	}
	if got.FormCues != "Knees out." {
		t.Errorf("FormCues = %q, want it kept — the movement is the same", got.FormCues)
	}
}

// Reps is free text because training is written that way, which means it needs
// its own bounds rather than a type to lean on.
func TestSetPrescriptionRejectsNonsense(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		sets int
		reps string
		rest int
	}{
		{"no sets", 0, "8-12", 90},
		{"negative sets", -1, "8-12", 90},
		{"no reps", 3, "", 90},
		{"blank reps", 3, "   ", 90},
		{"negative rest", 3, "8-12", -1},
		{"a rep range that is an essay", 3, strings.Repeat("x", maxRepsLength+1), 90},
	}

	for _, c := range cases {
		if _, err := SetPrescription(samplePlan(), 0, 0, c.sets, c.reps, c.rest); err == nil {
			t.Errorf("%s: accepted", c.name)
		}
	}
}

func TestSetPrescriptionTrimsTheRepRange(t *testing.T) {
	t.Parallel()

	out, err := SetPrescription(samplePlan(), 0, 0, 3, "  8-12  ", 90)
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if got := out.Days[0].Exercises[0].Reps; got != "8-12" {
		t.Errorf("Reps = %q, want it trimmed", got)
	}
}

func TestSetPrescriptionDoesNotDisturbTheOriginalPlan(t *testing.T) {
	t.Parallel()

	original := samplePlan()
	if _, err := SetPrescription(original, 0, 0, 9, "1", 300); err != nil {
		t.Fatalf("set: %v", err)
	}
	if got := original.Days[0].Exercises[0].Sets; got != 5 {
		t.Errorf("the original's sets are now %d", got)
	}
}
