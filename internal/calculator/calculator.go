// Package calculator turns a person's biometrics and stated activity/goal
// into a BMR/TDEE/macro target (Mifflin-St Jeor), and keeps the current one
// alongside a history of past plans.
package calculator

import "github.com/NorthAIProject/north-client/internal/calculator/macroplan"

// The plan shape lives in a leaf package so the service and any future
// template that renders one do not import each other.
type MacroPlan = macroplan.MacroPlan

const (
	ActivitySedentary  = macroplan.ActivitySedentary
	ActivityLight      = macroplan.ActivityLight
	ActivityModerate   = macroplan.ActivityModerate
	ActivityHeavy      = macroplan.ActivityHeavy
	ActivityExtraHeavy = macroplan.ActivityExtraHeavy

	GoalCutting     = macroplan.GoalCutting
	GoalMaintenance = macroplan.GoalMaintenance
	GoalBulking     = macroplan.GoalBulking

	SplitHighCarb     = macroplan.SplitHighCarb
	SplitModerateCarb = macroplan.SplitModerateCarb
	SplitLowCarb      = macroplan.SplitLowCarb
)

var (
	ActivityLevels = macroplan.ActivityLevels
	Goals          = macroplan.Goals
	Splits         = macroplan.Splits

	// What each choice actually means, so the form is not five adjectives and
	// three nouns with no explanation of what picking one costs.
	ActivityDescriptions = macroplan.ActivityDescriptions
	GoalDescriptions     = macroplan.GoalDescriptions
	SplitDescriptions    = macroplan.SplitDescriptions
)

// Display, WeightUnit, and HeightUnit render a stored plan's metric numbers in
// the units the person prefers (internal/preferences owns that setting).
var (
	Display    = macroplan.Display
	WeightUnit = macroplan.WeightUnit
	HeightUnit = macroplan.HeightUnit
	GoalLabel  = macroplan.GoalLabel
)

// AllGoals returns the daily calorie target for every goal at a given TDEE, so
// the page can show what each objective costs before one is chosen.
var AllGoals = macroplan.AllGoals
