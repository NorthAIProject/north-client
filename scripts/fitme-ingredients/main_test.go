package main

import (
	"strings"
	"testing"
)

// eggTuple is a real row from the source, in the source's real value order.
// The declared column order would read it as 9.7g of protein and 139g of
// fibre; the correct order gives an egg.
const eggTuple = `VALUES (gen_random_uuid(),
    ')egg',
    147,
    100,
    9.7,
    3.1,
    12.5,
    139,
    199,
    371,
    0.7,
    0,
    0.4
  );`

// This is the test the whole script exists for. Everything else is plumbing.
func TestParseReadsTheSourcesRealColumnOrder(t *testing.T) {
	t.Parallel()

	rows, err := parse(eggTuple)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}

	got := rows[0]
	for _, tt := range []struct {
		field     string
		got, want float64
	}{
		{"calories", got.Calories, 147},
		{"serving size", got.ServingSize, 100},
		{"total fat", got.FatTotal, 9.7},
		{"saturated fat", got.FatSaturated, 3.1},
		{"protein", got.Protein, 12.5},
		{"sodium", got.SodiumMg, 139},
		{"potassium", got.PotassiumMg, 199},
		{"cholesterol", got.CholesterolMg, 371},
		{"carbs", got.Carbs, 0.7},
		{"fiber", got.Fiber, 0},
		{"sugar", got.Sugar, 0.4},
	} {
		if tt.got != tt.want {
			t.Errorf("%s = %v, want %v", tt.field, tt.got, tt.want)
		}
	}
}

// A tuple whose field count differs is refused rather than guessed at.
// Guessing which field is missing would shift every value after it, which is
// the exact corruption this script was written to undo.
func TestParseRefusesATupleOfTheWrongShape(t *testing.T) {
	t.Parallel()

	short := `VALUES (gen_random_uuid(), ')egg', 147, 100, 9.7 );`
	if _, err := parse(short); err == nil {
		t.Fatal("a short tuple must be an error, not a partial row")
	}
}

func TestConvertKeepsAZeroCalorieFood(t *testing.T) {
	t.Parallel()

	// Salt is a real thing to log, and its sodium is the entire reason.
	// An earlier version of the gate dropped it for having no calories.
	salt := row{Name: ")salt", Calories: 0, ServingSize: 100, SodiumMg: 38395}

	got, report := convert([]row{salt})
	if len(got) != 1 {
		t.Fatalf("salt was dropped: %v", report.rejected)
	}
	if got[0].SodiumMg != 38395 {
		t.Errorf("sodium = %v, want it carried through", got[0].SodiumMg)
	}
}

func TestConvertRejectsARowWithNothingInIt(t *testing.T) {
	t.Parallel()

	_, report := convert([]row{{Name: ")nothing", ServingSize: 100}})
	if len(report.rejected) != 1 {
		t.Fatalf("an all-zero row should be rejected, got %v", report.rejected)
	}
}

// The gate's real job: catching a column that has shifted. A shift puts
// sodium — hundreds or thousands of milligrams — into a gram column, which no
// food can survive.
func TestConvertRejectsAShiftedRow(t *testing.T) {
	t.Parallel()

	shifted := row{
		Name: ")egg, misread", ServingSize: 100, Calories: 147,
		Protein:  139, // sodium landing in the protein column
		FatTotal: 12.5, Carbs: 0.7,
	}

	got, report := convert([]row{shifted})
	if len(got) != 0 {
		t.Errorf("a shifted row was accepted: %+v", got)
	}
	if len(report.rejected) != 1 || !strings.Contains(report.rejected[0], "protein+fat+carbs") {
		t.Errorf("expected the macro ceiling to fire, got %v", report.rejected)
	}
}

// Every real fat in the source lands just above 100g per 100g through
// rounding — olive oil is 101.2 — so the ceiling has to clear them.
func TestConvertAcceptsAPureFat(t *testing.T) {
	t.Parallel()

	oliveOil := row{Name: ")olive oil", ServingSize: 100, Calories: 869.2, FatTotal: 101.2, FatSaturated: 13.9}

	got, report := convert([]row{oliveOil})
	if len(got) != 1 {
		t.Fatalf("olive oil was rejected: %v", report.rejected)
	}
}

// The source stores everything per 100g. A row that does not would be
// misstated by whatever its serving happened to be, so it is refused rather
// than rescaled on an assumption.
func TestConvertRejectsARowThatIsNotPer100g(t *testing.T) {
	t.Parallel()

	_, report := convert([]row{{Name: ")something", ServingSize: 30, Calories: 100, Protein: 5}})
	if len(report.rejected) != 1 || !strings.Contains(report.rejected[0], "serving size") {
		t.Errorf("expected a serving-size rejection, got %v", report.rejected)
	}
}

func TestConvertDropsDuplicateNamesCaseInsensitively(t *testing.T) {
	t.Parallel()

	rows := []row{
		{Name: ")egg", ServingSize: 100, Calories: 147, Protein: 12.5},
		{Name: ")Egg", ServingSize: 100, Calories: 150, Protein: 12},
	}

	got, report := convert(rows)
	if len(got) != 1 {
		t.Fatalf("got %d ingredients, want 1", len(got))
	}
	if report.duplicates != 1 {
		t.Errorf("duplicates = %d, want 1", report.duplicates)
	}
	if got[0].Calories != 147 {
		t.Errorf("the first row should win, got %v calories", got[0].Calories)
	}
}

func TestCleanName(t *testing.T) {
	t.Parallel()

	tests := []struct{ in, want string }{
		{")protein", "Protein"},
		{"  hemp  ", "Hemp"},
		{")mahi   mahi", "Mahi mahi"},
		{"Egg", "Egg"},
	}

	for _, tt := range tests {
		if got := cleanName(tt.in); got != tt.want {
			t.Errorf("cleanName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestQuoteEscapesApostrophes(t *testing.T) {
	t.Parallel()

	if got, want := quote("Farmer's cheese"), "'Farmer''s cheese'"; got != want {
		t.Errorf("quote = %q, want %q", got, want)
	}
}
