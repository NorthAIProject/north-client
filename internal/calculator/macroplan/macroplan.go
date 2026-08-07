// Package macroplan holds the shape of a calculated calorie/macro target and
// the pure math behind it (Mifflin-St Jeor BMR, activity-scaled TDEE, and
// goal/split-adjusted macros).
//
// A leaf, so the calculator service and anything that renders a plan can both
// import it without importing each other. See CLAUDE.md on slice layout.
package macroplan

import (
	"fmt"
	"math"
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

// ActivityDescriptions says what each level means in training days per week.
//
// Without them the choice is five adjectives, and someone training four times
// a week has no way to tell "moderate" from "heavy" — which is a difference of
// several hundred calories a day.
var ActivityDescriptions = map[string]string{
	ActivitySedentary:  "Desk job, little activity beyond daily life.",
	ActivityLight:      "Training one or two days a week.",
	ActivityModerate:   "Training three to five days a week.",
	ActivityHeavy:      "Training six or seven days a week.",
	ActivityExtraHeavy: "Training twice a day, or physical work on top of training.",
}

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

// GoalLabel is the display name for a goal.
func GoalLabel(goal string) string { return goalLabels[goal] }

// GoalDescriptions state the rate of change each goal implies. The offsets
// above are the honest answer to "how fast", and quoting the rate is what
// stops someone picking Cutting and expecting it to be quicker.
var GoalDescriptions = map[string]string{
	GoalCutting:     "Lose weight, at roughly 0.45 kg (1 lb) a week.",
	GoalMaintenance: "Hold your current weight.",
	GoalBulking:     "Gain weight, at roughly 0.35 kg (0.75 lb) a week.",
}

// SplitDescriptions spell out the ratios behind each preset, so the choice is
// between numbers rather than between three adjectives.
var SplitDescriptions = map[string]string{
	SplitHighCarb:     "30% protein, 20% fat, 50% carbs.",
	SplitModerateCarb: "30% protein, 35% fat, 35% carbs.",
	SplitLowCarb:      "40% protein, 40% fat, 20% carbs.",
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

// UnitsMetric/UnitsImperial mirror preference.UnitsMetric/UnitsImperial's
// string values, and are not imported from there for the same reason
// SexMale/SexFemale above are not imported from biometrics: macroplan is a
// leaf with no dependency on other feature slices. The person's chosen system
// lives in internal/preferences and is read by the handler; nothing here
// stores one, because a second copy of that setting would be free to drift
// from the first.
const (
	UnitsMetric   = "metric"
	UnitsImperial = "imperial"
)

const (
	poundsPerKg = 1 / 0.45359237
	inchesPerCm = 1 / 2.54
)

// ToMetric converts a weight and height entered in the given units into
// kilograms and centimetres.
//
// Conversion happens at the edge, on the way in. Everything stored and
// everything computed below this line is metric, so no downstream code — and
// no stored row — has to carry which units it came from.
func ToMetric(weight, height float64, units string) (kg, cm float64) {
	if units == UnitsImperial {
		return weight / poundsPerKg, height / inchesPerCm
	}
	return weight, height
}

// Display is ToMetric's inverse, for showing a stored value back in the units
// it was entered in. The last step before a template, never an input to the
// maths.
func Display(kg, cm float64, units string) (weight, height float64) {
	if units == UnitsImperial {
		return round1(kg * poundsPerKg), round1(cm * inchesPerCm)
	}
	return kg, cm
}

// WeightUnit and HeightUnit are the labels that must accompany a Display
// result. Returned rather than hard-coded in a template so the conversion and
// its unit can never disagree.
func WeightUnit(units string) string {
	if units == UnitsImperial {
		return "lb"
	}
	return "kg"
}

func HeightUnit(units string) string {
	if units == UnitsImperial {
		return "in"
	}
	return "cm"
}

func round1(value float64) float64 { return math.Round(value*10) / 10 }

// AllGoals returns the daily calorie target for every goal at this TDEE.
//
// Shown before the choice is made: the difference between cutting and bulking
// is a concrete number of calories, and someone deciding between them should
// see both rather than pick one and discover the cost afterwards.
func AllGoals(tdee float64) map[string]float64 {
	targets := make(map[string]float64, len(Goals))
	for _, goal := range Goals {
		targets[goal] = CalorieGoalFor(tdee, goal)
	}
	return targets
}

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
