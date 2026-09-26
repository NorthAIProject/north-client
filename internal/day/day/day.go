// Package day holds the domain types for the "My Day" view: one local date,
// read across every slice that records something about it.
//
// Nothing here touches SQL or HTTP. The service in internal/day fills these
// types; the templates and the JSON API render them.
package day

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// RuleKind names one standing intention about the shape of a day.
type RuleKind string

const (
	RuleCaffeineCutoff RuleKind = "caffeine_cutoff"
	RuleKitchenCloses  RuleKind = "kitchen_closes"
	RuleLastDrink      RuleKind = "last_drink"
	RuleScreensOff     RuleKind = "screens_off"
)

// RuleKinds is every kind, in the order a settings form lists them. The one
// list the handler, the API and validation all read.
func RuleKinds() []RuleKind {
	return []RuleKind{RuleCaffeineCutoff, RuleKitchenCloses, RuleLastDrink, RuleScreensOff}
}

// Valid reports whether k is a kind this build knows.
func (k RuleKind) Valid() bool {
	for _, known := range RuleKinds() {
		if k == known {
			return true
		}
	}
	return false
}

// Label is the English marker text, used by the coach summary and as the
// fallback when the catalogue has no entry.
func (k RuleKind) Label() string {
	switch k {
	case RuleCaffeineCutoff:
		return "No caffeine"
	case RuleKitchenCloses:
		return "Kitchen closes"
	case RuleLastDrink:
		return "Last drink"
	case RuleScreensOff:
		return "Screens off"
	default:
		return string(k)
	}
}

// Rule is one standing intention: "the kitchen closes at 20:00".
type Rule struct {
	Kind    RuleKind
	At      string // "HH:MM"
	Enabled bool
}

// Validate checks a rule before it is stored.
func (r Rule) Validate() error {
	if !r.Kind.Valid() {
		return apperr.Wrap(apperr.ErrValidation, "unknown rule kind %q", r.Kind)
	}
	if _, err := ParseClock(r.At); err != nil {
		return err
	}
	return nil
}

// On places the rule on a calendar date, in that date's location.
func (r Rule) On(date time.Time) time.Time {
	mins, _ := ParseClock(r.At)
	y, m, d := date.Date()
	return time.Date(y, m, d, mins/60, mins%60, 0, 0, date.Location())
}

// Summary renders the rule for the coach.
func (r Rule) Summary() string {
	return fmt.Sprintf("%s at %s", r.Kind.Label(), r.At)
}

// ParseClock reads "HH:MM" into minutes after midnight.
func ParseClock(s string) (int, error) {
	h, m, ok := strings.Cut(s, ":")
	if !ok || len(h) != 2 || len(m) != 2 {
		return 0, apperr.Wrap(apperr.ErrValidation, "time must be HH:MM, got %q", s)
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, apperr.Wrap(apperr.ErrValidation, "time must be HH:MM, got %q", s)
	}
	return hh*60 + mm, nil
}

// Ring is one progress ring: a value against a goal.
type Ring struct {
	Value float64
	Goal  float64
}

// Fraction is progress in [0, 1]. Over-achievement is capped: a ring that has
// closed is closed, and drawing it past the start reads as a rendering bug.
func (r Ring) Fraction() float64 {
	if r.Goal <= 0 {
		return 0
	}
	return math.Min(r.Value/r.Goal, 1)
}

// Percent is Fraction as a whole number, uncapped for the label.
func (r Ring) Percent() int {
	if r.Goal <= 0 {
		return 0
	}
	return int(math.Round(r.Value / r.Goal * 100))
}

// Default activity goals, the same numbers Apple ships as its defaults. A
// person without their own goals gets rings that mean something on day one.
const (
	DefaultMoveGoalKcal     = 500
	DefaultExerciseGoalMins = 30
	DefaultStandGoalHours   = 12
)

// ActivityRings is the three-ring summary of movement.
type ActivityRings struct {
	Move     Ring // active kcal
	Exercise Ring // minutes
	Stand    Ring // hours
}

// Food is one day's intake against the macro plan.
type Food struct {
	Calories float64
	ProteinG float64
	CarbG    float64
	FatG     float64

	// Goal is zero when no macro plan has been generated; HasGoal says which.
	HasGoal      bool
	CalorieGoal  float64
	ProteinGoalG float64
	CarbGoalG    float64
	FatGoalG     float64
}

// Water is one day's drinking against the target.
type Water struct {
	TotalML  int
	TargetML int
}

// Fraction is progress toward the target, capped at 1.
func (w Water) Fraction() float64 {
	if w.TargetML <= 0 {
		return 0
	}
	return math.Min(float64(w.TotalML)/float64(w.TargetML), 1)
}

// SleepStage is one kind of sleep a device reports.
type SleepStage string

const (
	StageDeep  SleepStage = "deep"
	StageREM   SleepStage = "rem"
	StageCore  SleepStage = "core"
	StageAwake SleepStage = "awake"
)

// SleepStages lists the stages in the order a hypnogram draws them, top down.
func SleepStages() []SleepStage {
	return []SleepStage{StageAwake, StageREM, StageCore, StageDeep}
}

// SleepBlock is one contiguous stretch of a single stage.
type SleepBlock struct {
	Stage SleepStage
	Start time.Time
	End   time.Time
}

// Minutes is the block's length.
func (b SleepBlock) Minutes() int { return int(b.End.Sub(b.Start).Minutes()) }

// Sleep is the night that ends on the viewed date.
type Sleep struct {
	TotalMinutes int
	Start        *time.Time
	End          *time.Time

	// StageMinutes is empty when the only source is a manual log, which has a
	// duration and nothing inside it.
	StageMinutes map[SleepStage]int
	Blocks       []SleepBlock

	// Quality is the 1-5 manual rating, when there is one.
	Quality *int

	// Source is "manual" or the device provider that reported it.
	Source string
}

// HasStages reports whether there is a breakdown to draw.
func (s Sleep) HasStages() bool { return len(s.StageMinutes) > 0 }

// Duration renders the total as "7h 13m".
func (s Sleep) Duration() string { return FormatMinutes(s.TotalMinutes) }

// Workouts summarises the day's finished sessions.
type Workouts struct {
	Count    int
	Minutes  int
	Calories float64
	Labels   []string
}

// Body is the latest measurement and what it means.
type Body struct {
	WeightKg       *float64
	HeightCm       *float64
	BMI            *float64
	TargetWeightKg *float64

	BloodPressure *BloodPressure
	Soreness      []Soreness
}

// BMICategory is the WHO adult band for a BMI.
type BMICategory string

const (
	BMIUnderweight BMICategory = "underweight"
	BMIHealthy     BMICategory = "healthy"
	BMIOverweight  BMICategory = "overweight"
	BMIObese       BMICategory = "obese"
)

// CategoryFor places a BMI in its WHO band.
func CategoryFor(bmi float64) BMICategory {
	switch {
	case bmi < 18.5:
		return BMIUnderweight
	case bmi < 25:
		return BMIHealthy
	case bmi < 30:
		return BMIOverweight
	default:
		return BMIObese
	}
}

// Category is the band for the body's BMI, empty when there is none.
func (b Body) Category() BMICategory {
	if b.BMI == nil {
		return ""
	}
	return CategoryFor(*b.BMI)
}

// Marker is a rule placed on the viewed date, for the timeline.
type Marker struct {
	Kind RuleKind
	At   time.Time

	// Passed is true once the marker's time has gone by on today's view. Past
	// dates are entirely passed; future dates not at all.
	Passed bool
}

// FormatMinutes renders a duration as "7h 13m", "45m" or "8h".
func FormatMinutes(minutes int) string {
	if minutes < 60 {
		return fmt.Sprintf("%dm", minutes)
	}
	h, m := minutes/60, minutes%60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

// Caffeine is the day's caffeine: what was drunk, and what is still active.
type Caffeine struct {
	TotalMG  int
	ActiveMG int
	LimitMG  int
	// AfterCutoff is true when something was drunk after the day's caffeine
	// cutoff rule.
	AfterCutoff bool
}

// Fast is the fast shown on the day: the open one, or the latest that touched
// the date.
type Fast struct {
	StartedAt   time.Time
	EndedAt     *time.Time
	TargetHours int
	Elapsed     time.Duration
	Phase       string
	Fraction    float64
}

// Open reports whether the fast is still running.
func (f Fast) Open() bool { return f.EndedAt == nil }

// Nutrients is the day's micronutrient coverage.
type Nutrients struct {
	Covered []string
	Missing []string
}

// Total is the size of the tracked set.
func (n Nutrients) Total() int { return len(n.Covered) + len(n.Missing) }

// Soreness is one sore region.
type Soreness struct {
	Region   string
	Severity int
}

// Milestone is a "months since" tracker as the vitals strip shows it.
type Milestone struct {
	ID          string
	Name        string
	MonthsSince int
	Fraction    float64
	Due         bool
}

// BloodPressure is the latest reading.
type BloodPressure struct {
	Systolic  int
	Diastolic int
	At        time.Time
}

// LevelFor turns lifetime check-in days into a level: one level per five
// days, so the number keeps moving without a missed day ever taking it back.
func LevelFor(totalDays int) int { return totalDays/5 + 1 }

// ToGoal is how far the weight is from the target, always positive, and false
// when either is unknown.
func (b Body) ToGoal() (float64, bool) {
	if b.WeightKg == nil || b.TargetWeightKg == nil {
		return 0, false
	}
	return math.Round(math.Abs(*b.WeightKg-*b.TargetWeightKg)*10) / 10, true
}
