package meal

const (
	PlanTypeNoCarb   = "no_carb"
	PlanTypeLowCarb  = "low_carb"
	PlanTypeMidCarb  = "mid_carb"
	PlanTypeHighCarb = "high_carb"
	PlanTypeCustom   = "custom"
)

var PlanTypes = []string{
	PlanTypeNoCarb,
	PlanTypeLowCarb,
	PlanTypeMidCarb,
	PlanTypeHighCarb,
	PlanTypeCustom,
}

var planTypeLabels = map[string]string{
	PlanTypeNoCarb:   "No carb",
	PlanTypeLowCarb:  "Low carb",
	PlanTypeMidCarb:  "Mid carb",
	PlanTypeHighCarb: "High carb",
	PlanTypeCustom:   "Custom",
}

// PlanTypeLabel returns a human-readable name for a plan type.
func PlanTypeLabel(pt string) string {
	return planTypeLabels[pt]
}

// CarbRange returns the minimum and maximum carb percentage (0.0 to 1.0)
// of the active macro target for the given plan type.
func CarbRange(planType string) (minPct, maxPct float64) {
	switch planType {
	case PlanTypeNoCarb:
		return 0.0, 0.05
	case PlanTypeLowCarb:
		return 0.06, 0.25
	case PlanTypeMidCarb:
		return 0.26, 0.45
	case PlanTypeHighCarb:
		return 0.46, 0.65
	case PlanTypeCustom:
		return 0.0, 1.0
	default:
		return 0.0, 1.0
	}
}

// DefaultCarbPct returns the default carb percentage (0.0 to 1.0) for the given plan type.
func DefaultCarbPct(planType string) float64 {
	switch planType {
	case PlanTypeNoCarb:
		return 0.05
	case PlanTypeLowCarb:
		return 0.20
	case PlanTypeMidCarb:
		return 0.35
	case PlanTypeHighCarb:
		return 0.55
	default:
		return 1.0
	}
}

// DayTarget holds the resolved macro targets for a single day.
type DayTarget struct {
	Calories float64
	ProteinG float64
	FatG     float64
	CarbG    float64
}

// Overage reports how far consumed macros exceed their day target (positive = over).
type Overage struct {
	Calories float64
	ProteinG float64
	FatG     float64
	CarbG    float64
	IsOver   bool
}

// CheckOverage compares consumed macros against a day's target.
// Exceeding carb, protein, or fat marks IsOver = true.
func CheckOverage(consumed Macros, target DayTarget) Overage {
	diffProtein := consumed.ProteinG - target.ProteinG
	diffFat := consumed.FatG - target.FatG
	diffCarb := consumed.CarbG - target.CarbG
	diffCal := consumed.Calories - target.Calories

	isOver := diffProtein > 0.01 || diffFat > 0.01 || diffCarb > 0.01

	return Overage{
		Calories: diffCal,
		ProteinG: diffProtein,
		FatG:     diffFat,
		CarbG:    diffCarb,
		IsOver:   isOver,
	}
}

// ResolveDayTarget computes the macro targets for a single day within a meal plan,
// given the plan-level settings and any per-day overrides.
//
// Precedence:
//  1. Per-day custom grams (if dayCustomCarbG, dayCustomProteinG, or dayCustomFatG are set)
//  2. Per-day plan type (if dayPlanType is set, uses that range's default percentage)
//  3. Plan-level plan type (uses that range's default percentage or planCustomCarbPct)
//  4. No constraint (returns the active macro plan's targets as-is)
func ResolveDayTarget(
	baseProteinG, baseFatG, baseCarbG float64,
	planType string,
	planCustomCarbPct *float64,
	dayPlanType string,
	dayCustomCarbG, dayCustomProteinG, dayCustomFatG *float64,
) DayTarget {
	proteinG := baseProteinG
	fatG := baseFatG
	carbG := baseCarbG

	if dayCustomCarbG != nil {
		carbG = *dayCustomCarbG
	} else if dayPlanType != "" {
		carbG = resolveCarbGrams(baseCarbG, dayPlanType, nil)
	} else if planType != "" {
		carbG = resolveCarbGrams(baseCarbG, planType, planCustomCarbPct)
	}

	if dayCustomProteinG != nil {
		proteinG = *dayCustomProteinG
	}
	if dayCustomFatG != nil {
		fatG = *dayCustomFatG
	}

	return DayTarget{
		Calories: proteinG*4 + fatG*9 + carbG*4,
		ProteinG: proteinG,
		FatG:     fatG,
		CarbG:    carbG,
	}
}

func resolveCarbGrams(baseCarb float64, planType string, customPct *float64) float64 {
	if planType == PlanTypeCustom && customPct != nil {
		return baseCarb * (*customPct / 100)
	}
	return baseCarb * DefaultCarbPct(planType)
}
