package planimport

import (
	"reflect"
	"strings"
	"testing"
)

func intp(n int) *int           { return &n }
func floatp(f float64) *float64 { return &f }

func TestWorkoutFromRowsMapsColumnsWithoutInventingValues(t *testing.T) {
	t.Parallel()

	rows := [][]string{
		{"Coach Sam — Block 3"},
		{},
		{"Day", "Exercise Name", "Sets", "Reps", "Load (kg)", "Rest", "Notes", "Video"},
		{"Monday", "Back Squat", "3", "5", "100", "3 min", "brace hard", "https://x"},
		{"", "Romanian Deadlift", "", "8-10", "", "", "", ""},
		{"Day 2", "Bench Press", "4x6", "", "RPE 8", "1:30", "", ""},
		{"Day 2", "Pull-up", "3-4", "AMRAP", "bodyweight", "a while", "", ""},
		{"", "", "", "", "", "", "", ""},
		{"Day 2", "", "3", "10", "", "", "superset with above", ""},
	}

	draft, err := WorkoutFromRows("block3.xlsx", rows)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if draft.Name != "Coach Sam — Block 3" {
		t.Fatalf("name = %q, want the title line above the header", draft.Name)
	}
	if len(draft.Days) != 2 {
		t.Fatalf("days = %d, want 2", len(draft.Days))
	}

	mon := draft.Days[0]
	if mon.Label != "Monday" || mon.Weekday != "Monday" {
		t.Fatalf("day 1 = %q/%q", mon.Label, mon.Weekday)
	}
	squat := mon.Exercises[0]
	want := ExerciseDraft{Name: "Back Squat", Sets: intp(3), Reps: "5", Load: "100", RestSeconds: intp(180), Notes: "brace hard", Flags: []string{}}
	if !reflect.DeepEqual(squat, want) {
		t.Fatalf("squat = %+v, want %+v", squat, want)
	}

	// The blank day cell belongs to Monday, and every blank stays blank.
	rdl := mon.Exercises[1]
	if rdl.Sets != nil || rdl.RestSeconds != nil || rdl.Load != "" || rdl.Reps != "8-10" {
		t.Fatalf("rdl = %+v, want blanks left blank", rdl)
	}

	day2 := draft.Days[1]
	if day2.Weekday != "" {
		t.Fatalf("Day 2 weekday = %q, want empty until the person assigns one", day2.Weekday)
	}
	bench := day2.Exercises[0]
	if *bench.Sets != 4 || bench.Reps != "6" || *bench.RestSeconds != 90 || bench.Load != "RPE 8" {
		t.Fatalf("bench = %+v", bench)
	}

	pullup := day2.Exercises[1]
	if pullup.Sets != nil || pullup.RestSeconds != nil || len(pullup.Flags) != 2 {
		t.Fatalf("pull-up = %+v, want two flags and no guessed numbers", pullup)
	}

	if len(draft.Unparsed) != 1 || !strings.Contains(draft.Unparsed[0], "superset with above") {
		t.Fatalf("unparsed = %q, want the nameless row kept", draft.Unparsed)
	}
}

func TestWorkoutFromRowsNeedsAnExerciseColumn(t *testing.T) {
	t.Parallel()

	_, err := WorkoutFromRows("x.csv", [][]string{{"date", "steps"}, {"Mon", "9000"}})
	if ReasonOf(err) != ReasonNotAPlan {
		t.Fatalf("err = %v, want not a plan", err)
	}
	if !strings.Contains(err.Error(), "Exercise") {
		t.Fatalf("message %q does not say which column is missing", err)
	}
}

func TestWorkoutFromRowsWithoutDayColumnIsOneUnlabelledDay(t *testing.T) {
	t.Parallel()

	draft, err := WorkoutFromRows("full_body-a.csv", [][]string{{"exercise", "sets"}, {"Squat", "3"}, {"Row", "3"}})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if draft.Name != "full body a" || len(draft.Days) != 1 || len(draft.Days[0].Exercises) != 2 {
		t.Fatalf("draft = %+v", draft)
	}
}

func TestMealFromRowsKeepsStatedMacrosAndFlagsBadCells(t *testing.T) {
	t.Parallel()

	rows := [][]string{
		{"Day", "Meal", "Food", "Qty", "Unit", "Protein (g)", "Carbs (g)", "Fat (g)", "Cost"},
		{"Mon", "Breakfast", "Oats", "80", "g", "10", "54", "6", "0.40"},
		{"", "", "Milk", "250", "ml", "8", "12", "4", ""},
		{"", "Lunch", "Chicken breast", "150g", "", "46", "0", "5", ""},
		{"Tue", "Breakfast", "Eggs", "3", "", "lots", "", "", ""},
		{"Tue", "Breakfast", "Toast", "1 1/2", "slices", "", "", "", ""},
	}

	draft, err := MealFromRows("week.csv", rows)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(draft.Days) != 2 {
		t.Fatalf("days = %d", len(draft.Days))
	}

	mon := draft.Days[0]
	if mon.Weekday == nil || *mon.Weekday != 1 {
		t.Fatalf("Mon weekday = %v, want 1", mon.Weekday)
	}
	if len(mon.Meals) != 2 || mon.Meals[0].Name != "Breakfast" || len(mon.Meals[0].Foods) != 2 {
		t.Fatalf("Mon meals = %+v", mon.Meals)
	}

	oats := mon.Meals[0].Foods[0]
	if *oats.Quantity != 80 || oats.Unit != "g" || *oats.StatedProteinG != 10 || *oats.StatedCarbG != 54 || *oats.StatedFatG != 6 {
		t.Fatalf("oats = %+v", oats)
	}
	if oats.Grams != nil || oats.Macros != nil || oats.IngredientID != nil {
		t.Fatalf("oats = %+v, want grams and macros left to Preview", oats)
	}

	chicken := mon.Meals[1].Foods[0]
	if *chicken.Quantity != 150 || chicken.Unit != "g" {
		t.Fatalf("chicken = %+v, want 150 g split out of the quantity cell", chicken)
	}

	eggs := draft.Days[1].Meals[0].Foods[0]
	if eggs.StatedProteinG != nil || len(eggs.Flags) != 1 {
		t.Fatalf("eggs = %+v, want 'lots' flagged, not read", eggs)
	}
	toast := draft.Days[1].Meals[0].Foods[1]
	if *toast.Quantity != 1.5 || toast.Unit != "slices" {
		t.Fatalf("toast = %+v", toast)
	}
}

func TestWorkoutFromJSONAcceptsBothShapes(t *testing.T) {
	t.Parallel()

	bare := `[{"day":"Monday","exercises":[{"name":"Squat","sets":3,"reps":5,"load":"100kg","rest":"120s","notes":"depth"}]}]`
	named := `{"name":"Block 3","days":[{"day":1,"exercises":[{"exercise":"Squat","sets":"3","reps":"5","rest":120,"notes":null}]}]}`

	for _, raw := range []string{bare, named} {
		draft, err := WorkoutFromJSON("plan.json", []byte(raw))
		if err != nil {
			t.Fatalf("parse %s: %v", raw, err)
		}
		ex := draft.Days[0].Exercises[0]
		if ex.Name != "Squat" || *ex.Sets != 3 || ex.Reps != "5" || *ex.RestSeconds != 120 {
			t.Fatalf("exercise = %+v from %s", ex, raw)
		}
	}
}

func TestMealFromJSONAcceptsFoodRowsAndNamedMeals(t *testing.T) {
	t.Parallel()

	raw := `[
	  {"day":"Monday","meals":[
	    {"meal":"Breakfast","food":"Oats","quantity":80,"unit":"g","protein":10,"carbs":54,"fat":6},
	    {"name":"Lunch","foods":[{"food":"Rice","quantity":"200","unit":"g"}]}
	  ]}
	]`
	draft, err := MealFromJSON("plan.json", []byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	meals := draft.Days[0].Meals
	if len(meals) != 2 || meals[0].Name != "Breakfast" || meals[1].Name != "Lunch" || meals[1].Foods[0].Food != "Rice" {
		t.Fatalf("meals = %+v", meals)
	}
}

func TestJSONOfTheWrongPlanIsNotAPlan(t *testing.T) {
	t.Parallel()

	meal := `[{"day":"Monday","meals":[{"food":"Oats"}]}]`
	if _, err := WorkoutFromJSON("x.json", []byte(meal)); ReasonOf(err) != ReasonNotAPlan {
		t.Fatalf("workout from meal JSON: err = %v, want not a plan", err)
	}
	if _, err := MealFromJSON("x.json", []byte(`{"steps": 9000}`)); ReasonOf(err) != ReasonNotAPlan {
		t.Fatalf("meal from other JSON: err = %v, want not a plan", err)
	}
}

func TestCellParsers(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]*int{"90": intp(90), "90s": intp(90), "2 min": intp(120), "1.5 min": intp(90), "1:30": intp(90), "": nil, "long": nil} {
		got, _ := parseRest(raw)
		if (got == nil) != (want == nil) || (got != nil && *got != *want) {
			t.Errorf("parseRest(%q) = %v, want %v", raw, got, want)
		}
	}

	for raw, want := range map[string]*float64{"150": floatp(150), "1/2": floatp(0.5), "2,5": floatp(2.5), "1 1/2": floatp(1.5)} {
		got, _, ok := parseAmount(raw)
		if !ok || *got != *want {
			t.Errorf("parseAmount(%q) = %v, want %v", raw, got, *want)
		}
	}
}
