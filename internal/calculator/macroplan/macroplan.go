// Package macroplan holds the shape of a calculated calorie/macro target and
// the pure math behind it (Mifflin-St Jeor BMR, activity-scaled TDEE, and
// goal/split-adjusted macros).
//
// A leaf, so the calculator service and anything that renders a plan can both
// import it without importing each other. See CLAUDE.md on slice layout.
package macroplan

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Activity levels, matching the standard Mifflin-St Jeor activity multiplier
// scale.
const (
	ActivitySedentary  = "sedentary"
	ActivityLight      = "light"
	ActivityModerate   = "moderate"
	ActivityHeavy      = "heavy"
	ActivityExtraHeavy = "extra_heavy"
)

// ActivityLevels is the ordered set offered in the UI.
var ActivityLevels = []string{ActivitySedentary, ActivityLight, ActivityModerate, ActivityHeavy, ActivityExtraHeavy}

var activityMultipliers = map[string]float64{
	ActivitySedentary:  1.2,
	ActivityLight:      1.375,
	ActivityModerate:   1.55,
	ActivityHeavy:      1.725,
	ActivityExtraHeavy: 1.9,
}

// Goals a calculated plan can target. Flat kcal offsets rather than a
// percentage of TDEE: a fixed deficit/surplus is what actually corresponds to
// a fixed rate of weight change, regardless of how large someone's TDEE is.
const (
	GoalCutting     = "cutting"
	GoalMaintenance = "maintenance"
	GoalBulking     = "bulking"
)

// Goals is the ordered set offered in the UI.
var Goals = []string{GoalCutting, GoalMaintenance, GoalBulking}

var goalOffsets = map[string]float64{
	GoalCutting:     -450,
	GoalMaintenance: 0,
	GoalBulking:     350,
}

var goalLabels = map[string]string{
	GoalCutting:     "Cutting",
	GoalMaintenance: "Maintenance",
	GoalBulking:     "Bulking",
}

// Macro splits offered as presets rather than a free-form ratio: most people
// want a preset, not a spreadsheet.
const (
	SplitHighCarb     = "high_carb"
	SplitModerateCarb = "moderate_carb"
	SplitLowCarb      = "low_carb"
)

// Splits is the ordered set offered in the UI.
var Splits = []string{SplitHighCarb, SplitModerateCarb, SplitLowCarb}

type macroRatio struct{ Protein, Fat, Carb float64 }

var splitRatios = map[string]macroRatio{
	SplitHighCarb:     {Protein: 0.3, Fat: 0.2, Carb: 0.5},
	SplitModerateCarb: {Protein: 0.3, Fat: 0.35, Carb: 0.35},
	SplitLowCarb:      {Protein: 0.4, Fat: 0.4, Carb: 0.2},
}

var splitLabels = map[string]string{
	SplitHighCarb:     "high-carb",
	SplitModerateCarb: "moderate-carb",
	SplitLowCarb:      "low-carb",
}

// Calories per gram, used to convert a macro ratio of a calorie goal into
// grams.
const (
	kcalPerGramProteinOrCarb = 4
	kcalPerGramFat           = 9
)

// SexMale/SexFemale mirror biometrics.SexMale/SexFemale's string values. Not
// imported directly: macroplan is a leaf package with no dependency on other
// feature slices, so the values are duplicated here rather than shared.
const (
	SexMale   = "male"
	SexFemale = "female"
)

// BMR is the Mifflin-St Jeor basal metabolic rate estimate, in kcal/day.
// Callers must pass a validated sex (SexMale or SexFemale); anything else is
// treated as female, since that branch is the "else" below.
func BMR(weightKg, heightCm float64, age int, sex string) float64 {
	base := 10*weightKg + 6.25*heightCm - 5*float64(age)
	if sex == SexMale {
		return base + 5
	}
	return base - 161
}

// TDEE scales BMR by an activity multiplier. level must be one of
// ActivityLevels; an unrecognised level scales by zero rather than panicking,
// so callers must validate before calling.
func TDEE(bmr float64, level string) float64 {
	return bmr * activityMultipliers[level]
}

// CalorieGoalFor applies the flat kcal offset for the chosen goal.
func CalorieGoalFor(tdee float64, goal string) float64 {
	return tdee + goalOffsets[goal]
}

// Macros splits a calorie goal into grams of protein, fat, and carbs
// according to the chosen preset.
func Macros(calorieGoal float64, split string) (proteinG, fatG, carbG float64) {
	r := splitRatios[split]
	proteinG = r.Protein * calorieGoal / kcalPerGramProteinOrCarb
	fatG = r.Fat * calorieGoal / kcalPerGramFat
	carbG = r.Carb * calorieGoal / kcalPerGramProteinOrCarb
	return
}

// MacroPlan is a calculated calorie/macro target, snapshotting the inputs it
// was computed from so it explains itself later without a join back through
// biometrics history that may have since moved on.
type MacroPlan struct {
	ID     uuid.UUID
	UserID uuid.UUID

	WeightKg float64
	HeightCm float64
	Age      int
	Sex      string

	ActivityLevel string
	Goal          string
	MacroSplit    string

	BMR         float64
	TDEE        float64
	CalorieGoal float64
	ProteinG    float64
	FatG        float64
	CarbG       float64

	IsCurrent bool
	CreatedAt time.Time
}

// Generate computes a MacroPlan's outputs from its inputs. It does not set
// ID/UserID/IsCurrent/CreatedAt — those are the repository's concern.
func Generate(weightKg, heightCm float64, age int, sex, activityLevel, goal, macroSplit string) MacroPlan {
	bmr := BMR(weightKg, heightCm, age, sex)
	tdee := TDEE(bmr, activityLevel)
	calorieGoal := CalorieGoalFor(tdee, goal)
	proteinG, fatG, carbG := Macros(calorieGoal, macroSplit)

	return MacroPlan{
		WeightKg:      weightKg,
		HeightCm:      heightCm,
		Age:           age,
		Sex:           sex,
		ActivityLevel: activityLevel,
		Goal:          goal,
		MacroSplit:    macroSplit,
		BMR:           bmr,
		TDEE:          tdee,
		CalorieGoal:   calorieGoal,
		ProteinG:      proteinG,
		FatG:          fatG,
		CarbG:         carbG,
	}
}

// Summary renders a plan for the coach's context.
func (p MacroPlan) Summary() string {
	return fmt.Sprintf(
		"%s target: %.0f kcal/day (BMR %.0f, TDEE %.0f) — %s split → %.0fg protein / %.0fg fat / %.0fg carbs",
		goalLabels[p.Goal], p.CalorieGoal, p.BMR, p.TDEE, splitLabels[p.MacroSplit], p.ProteinG, p.FatG, p.CarbG,
	)
}
