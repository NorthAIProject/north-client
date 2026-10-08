package meal

import (
	"math"
	"time"

	"github.com/google/uuid"
)

// The rules a meal plan's days are held to. Everything that decides a day's
// target or whether a change goes over it lives here, so the web pages, the
// native API and the coach all get the same answer.
//
// The limits come from one place: the person's current macro plan from the
// calculator ("active" below). Nothing here derives macros from a body; it
// only takes shares of the active target.

// PlanType is a plan's carb level.
type PlanType string

const (
	NoCarb   PlanType = "no_carb"
	LowCarb  PlanType = "low_carb"
	MidCarb  PlanType = "mid_carb"
	HighCarb PlanType = "high_carb"
	// Custom takes the plan's own share of the active carb target, and is an
	// advanced-mode choice.
	Custom PlanType = "custom"
)

// Valid reports whether t is one of the five plan types.
func (t PlanType) Valid() bool {
	_, ok := BandFor(t)
	return ok || t == Custom
}

// Label is the plan type's name as people read it.
func (t PlanType) Label() string {
	if b, ok := BandFor(t); ok {
		return b.Label
	}
	if t == Custom {
		return "Custom"
	}
	return string(t)
}

// Mode is how much of a plan the person shapes by hand.
type Mode string

const (
	// Easy plans take a preset plan type and a number of days, every day at
	// the plan's default target, and refuse anything that goes over.
	Easy Mode = "easy"
	// Advanced plans allow a custom carb share, per-day overrides, and going
	// over once the person confirms the amount.
	Advanced Mode = "advanced"
)

// Valid reports whether m is Easy or Advanced.
func (m Mode) Valid() bool { return m == Easy || m == Advanced }

// CarbBand is a preset plan type's share of the active carb target, in percent.
type CarbBand struct {
	Type   PlanType
	Label  string
	MinPct float64
	MaxPct float64
}

// CarbBands are the preset plan types, lowest carb first.
var CarbBands = []CarbBand{
	{Type: NoCarb, Label: "No carb", MinPct: 0, MaxPct: 5},
	{Type: LowCarb, Label: "Low carb", MinPct: 6, MaxPct: 25},
	{Type: MidCarb, Label: "Mid carb", MinPct: 26, MaxPct: 45},
	{Type: HighCarb, Label: "High carb", MinPct: 46, MaxPct: 65},
}

// BandFor finds a preset plan type's band; Custom and unknown types have none.
func BandFor(t PlanType) (CarbBand, bool) {
	for _, b := range CarbBands {
		if b.Type == t {
			return b, true
		}
	}
	return CarbBand{}, false
}

// DefaultPct is the band's midpoint, the share a day takes by default.
func (b CarbBand) DefaultPct() float64 { return (b.MinPct + b.MaxPct) / 2 }

// CarbG is the band's default carb grams for an active carb target.
func (b CarbBand) CarbG(activeCarbG float64) float64 { return activeCarbG * b.DefaultPct() / 100 }

// WeekOrder is the order days are shown and filled in: Monday first.
var WeekOrder = []time.Weekday{
	time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday,
}

// MaxDays is the most days a plan holds: one per weekday.
var MaxDays = len(WeekOrder)

// EasyWeekdays is the weekdays an n-day plan covers: the first n from Monday.
// n is assumed to be between 1 and MaxDays.
func EasyWeekdays(n int) []time.Weekday { return WeekOrder[:n] }

// NextFreeWeekday is the first weekday, from Monday, not already taken.
func NextFreeWeekday(taken []time.Weekday) (time.Weekday, bool) {
	for _, wd := range WeekOrder {
		free := true
		for _, t := range taken {
			if t == wd {
				free = false
				break
			}
		}
		if free {
			return wd, true
		}
	}
	return 0, false
}

// PlanSettings are the plan-wide choices that set every day's default.
type PlanSettings struct {
	Type PlanType
	// CustomCarbPct is set only when Type is Custom: the share of the active
	// carb target, 0–100.
	CustomCarbPct *float64
	Mode          Mode
}

// DayOverride is an advanced-mode day's departure from the plan's default.
// Nil fields follow the plan. CarbType and CarbG are never both set.
type DayOverride struct {
	// CarbType makes this a lower- or higher-carb day: the band's midpoint
	// instead of the plan's.
	CarbType *PlanType
	CarbG    *float64
	ProteinG *float64
	FatG     *float64
}

// IsZero reports whether the day follows the plan in full.
func (o DayOverride) IsZero() bool {
	return o.CarbType == nil && o.CarbG == nil && o.ProteinG == nil && o.FatG == nil
}

// ResolveDayTarget is a day's target. Carbs come from, in order, the day's
// grams, the day's band, the plan's custom share, then the plan's band.
// Protein and fat are the active target unless the day overrides them. No
// value goes above the active target: the calculator is the ceiling even if
// it has been recalculated lower since an override was saved.
func ResolveDayTarget(active Macros, plan PlanSettings, day DayOverride) Macros {
	carbs := planCarbG(active.CarbG, plan)
	switch {
	case day.CarbG != nil:
		carbs = *day.CarbG
	case day.CarbType != nil:
		if b, ok := BandFor(*day.CarbType); ok {
			carbs = b.CarbG(active.CarbG)
		}
	}
	protein, fat := active.ProteinG, active.FatG
	if day.ProteinG != nil {
		protein = *day.ProteinG
	}
	if day.FatG != nil {
		fat = *day.FatG
	}
	return MacrosFromGrams(
		math.Min(protein, active.ProteinG),
		math.Min(fat, active.FatG),
		math.Min(carbs, active.CarbG),
	)
}

func planCarbG(activeCarbG float64, plan PlanSettings) float64 {
	if plan.Type == Custom && plan.CustomCarbPct != nil {
		return activeCarbG * *plan.CustomCarbPct / 100
	}
	if b, ok := BandFor(plan.Type); ok {
		return b.CarbG(activeCarbG)
	}
	return activeCarbG
}

// overTolerance is how far past a target a macro may go before it counts as
// over: anything that would display as "0 g over" is not.
const overTolerance = 0.5

// DayStatus is a day measured against its target.
type DayStatus struct {
	Target   Macros
	Consumed Macros
	// Remaining is Target minus Consumed, negative where the day is over.
	Remaining Macros
	// Over is how far past the target each macro is, zero where it is not.
	Over Macros
}

// StatusOf measures consumed against target.
func StatusOf(target, consumed Macros) DayStatus {
	remaining := Macros{
		Calories: target.Calories - consumed.Calories,
		ProteinG: target.ProteinG - consumed.ProteinG,
		FatG:     target.FatG - consumed.FatG,
		CarbG:    target.CarbG - consumed.CarbG,
	}
	return DayStatus{
		Target: target, Consumed: consumed, Remaining: remaining,
		Over: Macros{
			Calories: math.Max(0, -remaining.Calories),
			ProteinG: math.Max(0, -remaining.ProteinG),
			FatG:     math.Max(0, -remaining.FatG),
			CarbG:    math.Max(0, -remaining.CarbG),
		},
	}
}

// IsOver reports whether protein, fat or carbs are past the target. Calories
// follow from the three and are shown, not checked.
func (s DayStatus) IsOver() bool {
	return s.Over.ProteinG > overTolerance || s.Over.FatG > overTolerance || s.Over.CarbG > overTolerance
}

// PlanState is what the overage rule needs of a plan: its settings, and each
// day's override and what its meals add up to.
type PlanState struct {
	Settings PlanSettings
	Days     []DayState
}

// DayState is one day of a PlanState.
type DayState struct {
	ID       uuid.UUID
	Weekday  time.Weekday
	Override DayOverride
	Consumed Macros
}

// Statuses measures every day against its target, in the order of s.Days.
func (s PlanState) Statuses(active Macros) []DayStatus {
	out := make([]DayStatus, len(s.Days))
	for i, d := range s.Days {
		out[i] = StatusOf(ResolveDayTarget(active, s.Settings, d.Override), d.Consumed)
	}
	return out
}

// DayOverage is one day a change would take further over its target.
type DayOverage struct {
	DayID   uuid.UUID
	Weekday time.Weekday
	Status  DayStatus
}

// Verdict is the overage rule's answer for one change.
type Verdict struct {
	Allowed bool
	// CanConfirm is set on a refusal the person may override: an advanced
	// plan, asked without confirmation.
	CanConfirm bool
	// Over lists the days the change takes further over, with the amounts.
	Over []DayOverage
}

// CheckWrite decides whether a change to a plan may be saved. A change is an
// overage when it takes some day further over on protein, fat or carbs than
// it was before. Easy plans refuse it; advanced plans save it only once
// confirmed.
//
// before is nil when there is nothing to compare against: a plan being
// created, or one being switched to easy, where any day over its target is
// refused regardless of how it got there.
//
// Measuring growth rather than "is over" means removing food from an over
// day, or adding protein to a day that is over only on carbs, saves without
// asking again.
func CheckWrite(active Macros, before *PlanState, after PlanState, confirm bool) Verdict {
	previous := map[uuid.UUID]DayStatus{}
	if before != nil {
		for i, st := range before.Statuses(active) {
			previous[before.Days[i].ID] = st
		}
	}

	var over []DayOverage
	for i, st := range after.Statuses(active) {
		if grew(previous[after.Days[i].ID].Over, st.Over) {
			over = append(over, DayOverage{DayID: after.Days[i].ID, Weekday: after.Days[i].Weekday, Status: st})
		}
	}

	switch {
	case len(over) == 0:
		return Verdict{Allowed: true}
	case after.Settings.Mode == Advanced && confirm:
		return Verdict{Allowed: true, Over: over}
	default:
		return Verdict{CanConfirm: after.Settings.Mode == Advanced, Over: over}
	}
}

func grew(before, after Macros) bool {
	return after.ProteinG > before.ProteinG+overTolerance ||
		after.FatG > before.FatG+overTolerance ||
		after.CarbG > before.CarbG+overTolerance
}
