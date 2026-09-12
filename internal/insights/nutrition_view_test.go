package insights

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/calculator"
	"github.com/NorthAIProject/north-client/internal/meals"
	insightpages "github.com/NorthAIProject/north-client/web/insights"
)

func mustNutritionView(t *testing.T, data NutritionData) insightpages.NutritionView {
	t.Helper()
	view, err := buildNutritionView(data)
	if err != nil {
		t.Fatalf("buildNutritionView: %v", err)
	}
	return view
}

func fedWeek(t *testing.T, withGoal bool) NutritionData {
	t.Helper()
	rg := weekRange(t)
	days := make([]NutritionDay, 0, 3)
	for i, kcal := range []float64{2400, 2100, 2600} {
		days = append(days, NutritionDay{
			Date:   rg.Since.AddDate(0, 0, i),
			Macros: meals.Macros{Calories: kcal, ProteinG: 170, FatG: 70, CarbG: 250},
		})
	}
	data := NutritionData{Range: rg, Days: days, Entries: 9}
	if withGoal {
		data.HasGoal = true
		data.Goal = calculator.MacroPlan{CalorieGoal: 2400, ProteinG: 180, FatG: 80, CarbG: 260}
	}
	return data
}

func TestNutritionViewAveragesTheDaysLogged(t *testing.T) {
	// The headline is an average day, not the window's total: nobody eats
	// 7100 calories a day.
	view := mustNutritionView(t, fedWeek(t, true))

	if view.AvgCalories != "2367 kcal" {
		t.Errorf("AvgCalories = %q, want %q", view.AvgCalories, "2367 kcal")
	}
	if view.DaysLogged != 3 {
		t.Errorf("DaysLogged = %d, want 3", view.DaysLogged)
	}
}

func TestNutritionViewShowsTheGoalWhenThereIsOne(t *testing.T) {
	view := mustNutritionView(t, fedWeek(t, true))

	if !view.HasGoal {
		t.Fatal("HasGoal = false despite a plan")
	}
	if view.GoalCalories == "" {
		t.Error("the goal is not stated")
	}
}

func TestNutritionViewWithoutAPlanStillChartsIntake(t *testing.T) {
	// Somebody who has not run the calculator still gets their chart. They
	// just get no verdict on it.
	view := mustNutritionView(t, fedWeek(t, false))

	if view.HasGoal {
		t.Error("HasGoal = true with no plan")
	}
	if !view.HasData {
		t.Error("HasData = false despite three logged days")
	}
	if view.GoalCalories != "" {
		t.Errorf("stated a goal of %q with no plan", view.GoalCalories)
	}
}

func TestNutritionViewOfAnEmptyWindowSaysSo(t *testing.T) {
	view := mustNutritionView(t, NutritionData{Range: weekRange(t)})

	if view.HasData {
		t.Error("HasData = true with nothing logged")
	}
}

func TestNutritionViewSplitsMacrosForTheDonut(t *testing.T) {
	view := mustNutritionView(t, fedWeek(t, true))

	if !view.HasSplit {
		t.Fatal("HasSplit = false despite macros logged")
	}
	if view.MacroSplit == nil {
		t.Error("no donut option built")
	}
}
