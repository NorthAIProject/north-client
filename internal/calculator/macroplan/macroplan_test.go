package macroplan

import (
	"math"
	"testing"
)

func TestToMetricAndDisplayRoundTrip(t *testing.T) {
	t.Parallel()

	// A weight typed in pounds, stored in kilograms, and shown again must come
	// back as the number that was typed. Anything else and someone's weight
	// drifts every time they open the page.
	const enteredWeight, enteredHeight = 180.0, 71.0

	kg, cm := ToMetric(enteredWeight, enteredHeight, UnitsImperial)
	if math.Abs(kg-81.6) > 0.1 {
		t.Errorf("180 lb = %.2f kg, want about 81.6", kg)
	}
	if math.Abs(cm-180.3) > 0.1 {
		t.Errorf("71 in = %.2f cm, want about 180.3", cm)
	}

	weight, height := Display(kg, cm, UnitsImperial)
	if weight != enteredWeight {
		t.Errorf("round trip gave %v lb, want the %v that was entered", weight, enteredWeight)
	}
	if height != enteredHeight {
		t.Errorf("round trip gave %v in, want the %v that was entered", height, enteredHeight)
	}
}

func TestMetricValuesArePassedThroughUntouched(t *testing.T) {
	t.Parallel()

	kg, cm := ToMetric(82, 180, UnitsMetric)
	if kg != 82 || cm != 180 {
		t.Errorf("ToMetric(metric) = %v, %v; want the values unchanged", kg, cm)
	}

	weight, height := Display(82, 180, UnitsMetric)
	if weight != 82 || height != 180 {
		t.Errorf("Display(metric) = %v, %v; want the values unchanged", weight, height)
	}
}

// An unrecognised units string must behave as metric rather than silently
// converting: metric is what every caller falls back to.
func TestUnknownUnitsAreTreatedAsMetric(t *testing.T) {
	t.Parallel()

	kg, cm := ToMetric(82, 180, "furlongs")
	if kg != 82 || cm != 180 {
		t.Errorf("got %v, %v; want the values unchanged", kg, cm)
	}
	if WeightUnit("furlongs") != "kg" || HeightUnit("furlongs") != "cm" {
		t.Error("unknown units should label as metric")
	}
}

// Converting without the matching label, or the reverse, is how a page shows
// 81.6 lb. The two are asserted together for that reason.
func TestUnitLabelsMatchTheConversion(t *testing.T) {
	t.Parallel()

	if got, want := WeightUnit(UnitsImperial), "lb"; got != want {
		t.Errorf("WeightUnit(imperial) = %q, want %q", got, want)
	}
	if got, want := HeightUnit(UnitsImperial), "in"; got != want {
		t.Errorf("HeightUnit(imperial) = %q, want %q", got, want)
	}
	if got, want := WeightUnit(UnitsMetric), "kg"; got != want {
		t.Errorf("WeightUnit(metric) = %q, want %q", got, want)
	}
	if got, want := HeightUnit(UnitsMetric), "cm"; got != want {
		t.Errorf("HeightUnit(metric) = %q, want %q", got, want)
	}
}

func TestAllGoalsMatchesTheChosenGoalsTarget(t *testing.T) {
	t.Parallel()

	const tdee = 2500

	targets := AllGoals(tdee)
	if len(targets) != len(Goals) {
		t.Fatalf("got %d targets, want one per goal (%d)", len(targets), len(Goals))
	}

	// The preview and the plan must agree. If they can diverge, someone picks
	// a goal from the comparison and gets a different number.
	for _, goal := range Goals {
		if want := CalorieGoalFor(tdee, goal); targets[goal] != want {
			t.Errorf("AllGoals[%q] = %v, want %v from CalorieGoalFor", goal, targets[goal], want)
		}
	}

	if targets[GoalCutting] >= targets[GoalMaintenance] {
		t.Error("cutting should be below maintenance")
	}
	if targets[GoalBulking] <= targets[GoalMaintenance] {
		t.Error("bulking should be above maintenance")
	}
}

// A missing description leaves an option labelled with a bare adjective, which
// is the problem the descriptions were added to solve.
func TestEveryChoiceHasADescription(t *testing.T) {
	t.Parallel()

	for _, level := range ActivityLevels {
		if ActivityDescriptions[level] == "" {
			t.Errorf("activity level %q has no description", level)
		}
	}
	for _, goal := range Goals {
		if GoalDescriptions[goal] == "" {
			t.Errorf("goal %q has no description", goal)
		}
		if GoalLabel(goal) == "" {
			t.Errorf("goal %q has no label", goal)
		}
	}
	for _, split := range Splits {
		if SplitDescriptions[split] == "" {
			t.Errorf("macro split %q has no description", split)
		}
	}
}
