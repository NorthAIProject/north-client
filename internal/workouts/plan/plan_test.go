package plan

import (
	"strings"
	"testing"
)

func TestSummaryIncludesMuscleEmphasis(t *testing.T) {
	t.Parallel()

	squat := exercise("Barbell Back Squat", "barbell")
	squat.Primary = []string{"quads", "glutes"}
	legPress := exercise("Leg Press", "machine")
	legPress.Primary = []string{"quads"} // duplicate of squat's — must not repeat

	p := planWith(day("Monday", squat, legPress))

	summary := p.Summary()
	if !strings.Contains(summary, "(emphasis: quads, glutes)") {
		t.Fatalf("expected deduplicated, order-preserving emphasis in summary, got: %q", summary)
	}
}

func TestSummaryOmitsEmphasisWhenNoExerciseHasPrimaryMuscles(t *testing.T) {
	t.Parallel()

	p := planWith(day("Monday", exercise("Push-up", "none")))

	if summary := p.Summary(); strings.Contains(summary, "emphasis") {
		t.Fatalf("expected no emphasis clause when Primary is unset, got: %q", summary)
	}
}

func TestPlanSchemaConstrainsMuscleFieldsToTheCanonicalTaxonomy(t *testing.T) {
	t.Parallel()

	schema := PlanSchema()
	exerciseSchema := schema.Properties["days"].Items.Properties["exercises"].Items

	for _, field := range []string{"primary_muscles", "secondary_muscles", "stabilizer_muscles"} {
		prop, ok := exerciseSchema.Properties[field]
		if !ok {
			t.Fatalf("exercise schema is missing %q", field)
		}
		if prop.Items == nil || len(prop.Items.Enum) != len(MuscleGroups) {
			t.Fatalf("%q should be an array constrained to MuscleGroups, got items=%+v", field, prop.Items)
		}
	}
}
