package insights

import (
	"fmt"

	"github.com/NorthAIProject/north-client/internal/insights/highlight"
	"github.com/NorthAIProject/north-client/internal/shared/viz"
	insightpages "github.com/NorthAIProject/north-client/web/insights"
)

func buildNutritionView(data NutritionData) (insightpages.NutritionView, error) {
	loc := data.Range.Location()
	labels := bucketLabels(data.Range)

	kcal := make([]point, 0, len(data.Days))
	var totalKcal, totalProtein, fat, carb float64
	for _, d := range data.Days {
		kcal = append(kcal, point{At: d.Date.In(loc), Value: d.Macros.Calories})
		totalKcal += d.Macros.Calories
		totalProtein += d.Macros.ProteinG
		fat += d.Macros.FatG
		carb += d.Macros.CarbG
	}

	// Averaged, not summed: a week bucket holds an average day, because
	// nobody eats 7100 calories in a day and a chart that said so would be
	// unreadable beside a goal line.
	series := bucketedMean(data.Range, kcal)

	view := insightpages.NutritionView{
		Range:         rangeView(data.Range),
		DaysLogged:    len(data.Days),
		Entries:       data.Entries,
		HasGoal:       data.HasGoal,
		CaloriesChart: viz.Bar("insights-nutrition-calories", "kcal", labels, series),
		HasData:       len(data.Days) > 0,
	}

	if n := float64(len(data.Days)); n > 0 {
		view.AvgCalories = fmt.Sprintf("%.0f kcal", totalKcal/n)
		view.AvgProtein = fmt.Sprintf("%.0f g", totalProtein/n)
	}
	if data.HasGoal {
		view.GoalCalories = fmt.Sprintf("%.0f kcal", data.Goal.CalorieGoal)
		view.GoalProtein = fmt.Sprintf("%.0f g", data.Goal.ProteinG)
	}

	// Grams, not calories: the donut answers "what did this come from", and
	// the three macros are what somebody adjusts.
	segments := []viz.DonutSegment{
		{Label: "Protein", Value: int(totalProtein)},
		{Label: "Fat", Value: int(fat)},
		{Label: "Carbs", Value: int(carb)},
	}
	split, err := option(viz.DonutOptionJSON(segments))
	if err != nil {
		return insightpages.NutritionView{}, err
	}
	view.MacroSplit = split
	view.HasSplit = totalProtein+fat+carb > 0

	view.Highlights = highlight.Find(highlight.Input{
		Series: []highlight.Series{{
			Label: "Calories", Unit: " kcal", Decimals: 0,
			Period: periodNoun(data.Range), Labels: labels, Values: series,
		}},
	}, maxHighlights)

	return view, nil
}
