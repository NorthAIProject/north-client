package main

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

// The mapping tables are the whole point of this script — the generated SQL is
// just their output. A muscle key that is not canonical produces a catalog row
// the viewer silently ignores and sanitizeMuscleGroups silently drops, which
// looks exactly like an exercise that trains nothing.

func TestEveryMappedMuscleIsCanonical(t *testing.T) {
	t.Parallel()

	for source, key := range muscleMap {
		if !plan.IsMuscleGroup(key) {
			t.Errorf("muscleMap[%q] = %q, which is not in plan.MuscleGroups", source, key)
		}
	}
}

func TestEveryCuratedSecondaryIsCanonical(t *testing.T) {
	t.Parallel()

	for slug, keys := range secondaryMuscles {
		for _, key := range keys {
			if !plan.IsMuscleGroup(key) {
				t.Errorf("secondaryMuscles[%q] contains %q, which is not in plan.MuscleGroups", slug, key)
			}
		}
	}
}

// The catalog's equipment values feed both the browse filter and the prompt's
// candidate list, and are checked against the same vocabulary the plan
// validator uses. A value outside it means the validator would reject an
// exercise the catalog had just recommended.
func TestEveryMappedEquipmentIsRecognisedByThePlanValidator(t *testing.T) {
	t.Parallel()

	// "none" and "other" are the two deliberate non-equipment values: nothing
	// required, and something the validator has no rule for.
	allowed := map[string]bool{"none": true, "other": true}
	for _, name := range plan.EquipmentNames() {
		allowed[name] = true
	}

	for source, mapped := range equipmentMap {
		if !allowed[mapped] {
			t.Errorf("equipmentMap[%q] = %q, which the plan validator does not recognise (known: %v)", source, mapped, plan.EquipmentNames())
		}
	}
}

func TestSlugify(t *testing.T) {
	t.Parallel()

	tests := []struct{ in, want string }{
		{"Barbell Bench Press - Medium Grip", "barbell-bench-press-medium-grip"},
		{"Rocky Pull-Ups/Pulldowns", "rocky-pull-ups-pulldowns"},
		{"T-Bar Row", "t-bar-row"},
		{"EZ-bar spider curl", "ez-bar-spider-curl"},
		{"Power snatch-", "power-snatch"},
		{"  Front   Squat  ", "front-squat"},
	}

	for _, tt := range tests {
		if got := slugify(tt.in); got != tt.want {
			t.Errorf("slugify(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// The stray ')' prefixes all 304 source names; missing it would put it in
// every slug and every display name.
func TestCleanNameStripsTheSourceArtifact(t *testing.T) {
	t.Parallel()

	if got := cleanName(")Incline Hammer Curls"); got != "Incline Hammer Curls" {
		t.Errorf("cleanName = %q, want %q", got, "Incline Hammer Curls")
	}
	if got := cleanName("Barbell Deadlift"); got != "Barbell Deadlift" {
		t.Errorf("cleanName must leave a clean name alone, got %q", got)
	}
}

func TestParseReadsATupleWithEscapedQuotes(t *testing.T) {
	t.Parallel()

	source := `INSERT INTO exercise_list(...) VALUES (gen_random_uuid(),
        ')Farmer''s Walk',
        'strongman',
        'forearms',
        'other',
        'intermediate',
        'Grip the handles. Don''t drop them.',
        'https://youtu.be/abc',
        false, now(), null );`

	rows, err := parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if want := ")Farmer's Walk"; rows[0].Name != want {
		t.Errorf("Name = %q, want %q", rows[0].Name, want)
	}
	if want := "Grip the handles. Don't drop them."; rows[0].Instructions != want {
		t.Errorf("Instructions = %q, want %q", rows[0].Instructions, want)
	}
}

// Repeated rows are the source's own artifact: it was fetched one muscle at a
// time, so a movement is listed under each muscle it trains. Merging keeps
// every attribution; dropping would keep whichever copy happened to come first.
func TestConvertMergesRowsThatShareASlug(t *testing.T) {
	t.Parallel()

	rows := []row{
		{Name: ")Power Snatch", Type: "olympic_weightlifting", Muscle: "hamstrings", Equipment: "barbell", Difficulty: "beginner", Instructions: "short"},
		{Name: ")Power Snatch", Type: "olympic_weightlifting", Muscle: "quadriceps", Equipment: "barbell", Difficulty: "expert", Instructions: "a much longer description of the lift"},
	}

	got, report := convert(rows)
	if len(got) != 1 {
		t.Fatalf("got %d exercises, want 1 merged", len(got))
	}
	if report.repeatedRows != 1 {
		t.Errorf("repeatedRows = %d, want 1", report.repeatedRows)
	}

	e := got[0]
	if want := []string{"hamstrings", "quads"}; !equal(e.Primary, want) {
		t.Errorf("Primary = %v, want %v — both attributions must survive", e.Primary, want)
	}
	if e.Instructions != "a much longer description of the lift" {
		t.Errorf("the fullest instructions should win, got %q", e.Instructions)
	}
}

func TestConvertKeepsPrimaryAndSecondaryDisjoint(t *testing.T) {
	t.Parallel()

	// barbell-deadlift's curated secondaries include "hamstrings", and the
	// source files it under hamstrings too. A muscle in both lists would be
	// drawn twice at two intensities, and the weaker one could win.
	rows := []row{{Name: ")Barbell Deadlift", Type: "powerlifting", Muscle: "hamstrings", Equipment: "barbell", Difficulty: "intermediate"}}

	got, _ := convert(rows)
	if len(got) != 1 {
		t.Fatalf("got %d exercises, want 1", len(got))
	}

	for _, secondary := range got[0].Secondary {
		for _, primary := range got[0].Primary {
			if secondary == primary {
				t.Errorf("%q appears in both Primary and Secondary", secondary)
			}
		}
	}
}

func equal(a, b []string) bool {
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
