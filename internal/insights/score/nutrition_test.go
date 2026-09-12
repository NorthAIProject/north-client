package score

import "testing"

func fed(days int, kcal, protein float64) []NutritionDay {
	out := make([]NutritionDay, days)
	for i := range out {
		out[i] = NutritionDay{Calories: kcal, ProteinG: protein}
	}
	return out
}

func TestNutritionWeightsSumToOneHundred(t *testing.T) {
	total := 0
	for _, c := range Nutrition(NutritionInput{}).Components {
		total += c.Weight
	}
	if total != 100 {
		t.Errorf("weights sum to %d, want 100", total)
	}
}

func TestNutritionWithoutAGoalCannotJudgeIntake(t *testing.T) {
	// Logging 2400 kcal is neither good nor bad until somebody says what they
	// were aiming for. Only the logging habit itself is measurable.
	got := Nutrition(NutritionInput{Days: fed(7, 2400, 150), WindowDays: 7})

	if component(t, got, "calories").Known {
		t.Error("calories scored with no goal set")
	}
	if component(t, got, "protein").Known {
		t.Error("protein scored with no goal set")
	}
	if !component(t, got, "food_logging").Known {
		t.Error("logging is measurable without a goal and should be Known")
	}
}

func TestNutritionScoresAWellFedWeek(t *testing.T) {
	got := Nutrition(NutritionInput{
		Days:        fed(7, 2400, 180),
		CalorieGoal: 2400,
		ProteinGoal: 180,
		WindowDays:  7,
	})

	if !got.HasData {
		t.Fatal("HasData = false")
	}
	if got.Points != 100 {
		t.Errorf("Points = %d, want 100", got.Points)
	}
}

func TestNutritionPenalisesOvershootAsWellAsUndershoot(t *testing.T) {
	// Calories are a target to sit near, not a number to maximise. Eating
	// double the goal must not score the same as hitting it.
	on := component(t, Nutrition(NutritionInput{
		Days: fed(7, 2400, 0), CalorieGoal: 2400, WindowDays: 7,
	}), "calories")
	over := component(t, Nutrition(NutritionInput{
		Days: fed(7, 4800, 0), CalorieGoal: 2400, WindowDays: 7,
	}), "calories")
	under := component(t, Nutrition(NutritionInput{
		Days: fed(7, 1200, 0), CalorieGoal: 2400, WindowDays: 7,
	}), "calories")

	if on.Earned != on.Weight {
		t.Errorf("hitting the goal earned %d of %d, want full marks", on.Earned, on.Weight)
	}
	if over.Earned >= on.Earned {
		t.Errorf("double the goal earned %d, on-target earned %d", over.Earned, on.Earned)
	}
	if under.Earned >= on.Earned {
		t.Errorf("half the goal earned %d, on-target earned %d", under.Earned, on.Earned)
	}
}

func TestNutritionTreatsProteinAsAFloorNotATarget(t *testing.T) {
	// Unlike calories, beating the protein goal is not a miss.
	on := component(t, Nutrition(NutritionInput{
		Days: fed(7, 0, 180), ProteinGoal: 180, WindowDays: 7,
	}), "protein")
	over := component(t, Nutrition(NutritionInput{
		Days: fed(7, 0, 220), ProteinGoal: 180, WindowDays: 7,
	}), "protein")

	if over.Earned != on.Earned {
		t.Errorf("exceeding protein earned %d, hitting it earned %d — want the same",
			over.Earned, on.Earned)
	}
}

func TestNutritionWithNothingLoggedHasNoData(t *testing.T) {
	got := Nutrition(NutritionInput{CalorieGoal: 2400, WindowDays: 7})

	if got.HasData {
		t.Errorf("HasData = true with nothing logged (coverage %d)", got.Coverage)
	}
}
