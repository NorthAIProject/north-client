package score

import "math"

// Weights for the nutrition domain. They sum to 100 — see the weights test.
const (
	nutritionWeightCalories = 40
	nutritionWeightProtein  = 30
	nutritionWeightLogging  = 30
)

// foodLogTargetShare is how much of the window has to carry a food log before
// the habit earns full marks. The same five-days-in-seven the check-in
// component asks for, and for the same reason: perfect is not the ask.
const foodLogTargetShare = 5.0 / 7.0

// NutritionDay is one day's logged intake. Plain numbers rather than the meals
// package's Macros, so this package stays importable from anywhere.
type NutritionDay struct {
	Calories float64
	ProteinG float64
}

// NutritionInput is the window's food logs and the plan they are measured
// against. A zero goal means the person has not set one.
type NutritionInput struct {
	Days []NutritionDay

	CalorieGoal float64
	ProteinGoal float64

	WindowDays int
}

// Nutrition reads how close intake sat to the plan, and how often it was
// logged at all.
//
// Without a macro plan only the logging habit is measurable: 2400 kcal is
// neither good nor bad until somebody says what they were aiming for, and
// scoring it anyway would be inventing a target on their behalf.
func Nutrition(in NutritionInput) Score {
	return New("nutrition", []Component{
		caloriesComponent(in),
		proteinComponent(in),
		foodLoggingComponent(in),
	})
}

func caloriesComponent(in NutritionInput) Component {
	c := Component{Key: "calories", Weight: nutritionWeightCalories}
	if in.CalorieGoal <= 0 || len(in.Days) == 0 {
		return c
	}

	// Proximity, not attainment. Calories are a number to sit near: eating
	// double the goal is a miss in the same way eating half of it is, and a
	// component that only looked at "did they reach it" would reward the
	// first and punish the second.
	var share float64
	for _, d := range in.Days {
		miss := math.Abs(d.Calories-in.CalorieGoal) / in.CalorieGoal
		share += math.Max(0, 1-miss)
	}

	c.Known = true
	c.Earned = award(c.Weight, share/float64(len(in.Days)))
	return c
}

func proteinComponent(in NutritionInput) Component {
	c := Component{Key: "protein", Weight: nutritionWeightProtein}
	if in.ProteinGoal <= 0 || len(in.Days) == 0 {
		return c
	}

	// A floor rather than a target: unlike calories, beating the protein goal
	// is not a miss, so the share is capped rather than measured both ways.
	var share float64
	for _, d := range in.Days {
		share += math.Min(1, d.ProteinG/in.ProteinGoal)
	}

	c.Known = true
	c.Earned = award(c.Weight, share/float64(len(in.Days)))
	return c
}

func foodLoggingComponent(in NutritionInput) Component {
	c := Component{Key: "food_logging", Weight: nutritionWeightLogging}
	if len(in.Days) == 0 || in.WindowDays <= 0 {
		return c
	}

	c.Known = true
	share := float64(len(in.Days)) / float64(in.WindowDays)
	c.Earned = award(c.Weight, share/foodLogTargetShare)
	return c
}
