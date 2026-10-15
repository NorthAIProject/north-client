// Package progresscheck holds the shape of a progress check: a person's
// weight and a few circumferences on a date, with photos as evidence, taken
// on a cadence they chose.
//
// It is a tracking record, not an assessment. Nothing here judges a number;
// it stores what the person confirmed and works out the figure's shape from it.
package progresscheck

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/bodymap"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Cadence is how often a person means to check.
type Cadence string

const (
	Weekly   Cadence = "weekly"
	Biweekly Cadence = "biweekly"
	Monthly  Cadence = "monthly"
)

// Cadences is the set offered, shortest first.
var Cadences = []Cadence{Weekly, Biweekly, Monthly}

// ParseCadence accepts a cadence as a client sends it.
func ParseCadence(s string) (Cadence, bool) {
	c := Cadence(strings.ToLower(strings.TrimSpace(s)))
	switch c {
	case Weekly, Biweekly, Monthly:
		return c, true
	}
	return "", false
}

// Next is the day the check after one taken on date falls due. A month is a
// calendar month, so a check on the 15th is next due on the 15th.
func (c Cadence) Next(date time.Time) time.Time {
	switch c {
	case Biweekly:
		return date.AddDate(0, 0, 14)
	case Monthly:
		return date.AddDate(0, 1, 0)
	default:
		return date.AddDate(0, 0, 7)
	}
}

// Pose is which way the person faces the camera.
type Pose string

const (
	Front Pose = "front"
	Side  Pose = "side"
	Back  Pose = "back"
)

// Poses is every pose, in the order a check shows them.
var Poses = []Pose{Front, Side, Back}

// ParsePose accepts a pose name.
func ParsePose(s string) (Pose, bool) {
	p := Pose(strings.ToLower(strings.TrimSpace(s)))
	switch p {
	case Front, Side, Back:
		return p, true
	}
	return "", false
}

// Source says who produced the numbers. Either way the person confirmed them.
const (
	SourceManual   = "manual"
	SourceAssisted = "assisted"
)

// Confidence values an assisted estimate can carry.
var Confidences = []string{"low", "medium", "high"}

// Photo is one pose of a check, stored privately in media.
type Photo struct {
	Pose    Pose
	MediaID uuid.UUID
}

// Measurements are the numbers of a check, metric. Nil is not measured.
type Measurements struct {
	WeightKg *float64
	WaistCm  *float64
	HipsCm   *float64
	ChestCm  *float64
	ThighCm  *float64
	ArmCm    *float64
}

// Check is one saved progress check.
type Check struct {
	ID         uuid.UUID
	MeasuredOn time.Time
	Measurements

	// WeightCarried marks a weight copied from the last known one because the
	// person gave none; a trend must not read it as a new reading.
	WeightCarried bool

	Source           string
	AssistConfidence *string
	AssistNotes      string

	Photos    []Photo
	CreatedAt time.Time
}

// Photo returns the check's photo for a pose.
func (c Check) Photo(p Pose) (Photo, bool) {
	for _, ph := range c.Photos {
		if ph.Pose == p {
			return ph, true
		}
	}
	return Photo{}, false
}

// Complete reports a check with both required photos, front and side.
func (c Check) Complete() bool {
	_, front := c.Photo(Front)
	_, side := c.Photo(Side)
	return front && side
}

// Input is a check as submitted, before photos are stored.
type Input struct {
	MeasuredOn time.Time
	Measurements

	Source           string
	AssistConfidence string
	AssistNotes      string
}

// bound is a measurement's plausible range, the same as the table's CHECK.
type bound struct {
	field    string
	label    string
	min, max float64
}

var bounds = []bound{
	{"weight_kg", "Weight", 20, 400},
	{"waist_cm", "Waist", 40, 250},
	{"hips_cm", "Hips", 50, 250},
	{"chest_cm", "Chest", 50, 250},
	{"thigh_cm", "Thigh", 25, 120},
	{"arm_cm", "Arm", 15, 80},
}

func (m Measurements) values() []*float64 {
	return []*float64{m.WeightKg, m.WaistCm, m.HipsCm, m.ChestCm, m.ThighCm, m.ArmCm}
}

// Empty reports a check with no number at all.
func (m Measurements) Empty() bool {
	for _, v := range m.values() {
		if v != nil {
			return false
		}
	}
	return true
}

// Validate checks a submission against today in the person's zone. Every
// number is optional; each one given must be plausible, rounded to a tenth.
func Validate(in Input, today time.Time) (Input, error) {
	var errs apperr.FieldErrors

	if in.MeasuredOn.IsZero() {
		in.MeasuredOn = today
	}
	if in.MeasuredOn.After(today) {
		errs = errs.Add("measured_on", "That date is in the future.")
	}

	for i, v := range in.values() {
		if v == nil {
			continue
		}
		b := bounds[i]
		if math.IsNaN(*v) || *v < b.min || *v > b.max {
			errs = errs.Add(b.field, fmt.Sprintf("%s should be between %.0f and %.0f.", b.label, b.min, b.max))
			continue
		}
		*v = math.Round(*v*10) / 10
	}

	switch in.Source {
	case "":
		in.Source = SourceManual
	case SourceManual, SourceAssisted:
	default:
		errs = errs.Add("source", "Source is manual or assisted.")
	}
	in.AssistNotes = strings.TrimSpace(in.AssistNotes)
	if len([]rune(in.AssistNotes)) > 280 {
		in.AssistNotes = string([]rune(in.AssistNotes)[:280])
	}
	if in.Source == SourceManual {
		in.AssistConfidence, in.AssistNotes = "", ""
	}
	if in.AssistConfidence != "" && !validConfidence(in.AssistConfidence) {
		errs = errs.Add("assist_confidence", "Confidence is low, medium or high.")
	}

	return in, errs.OrNil()
}

func validConfidence(c string) bool {
	for _, ok := range Confidences {
		if c == ok {
			return true
		}
	}
	return false
}

// Schedule is the cadence a person chose and the next due date.
type Schedule struct {
	Cadence   Cadence
	NextDueOn time.Time
}

// Due reports whether a check is due on today. A schedule never set is
// never due: the prompt is something a person asked for.
func (s Schedule) Due(today time.Time) bool {
	return s.Cadence != "" && !today.Before(s.NextDueOn)
}

// referenceHeightCm stands in for a height nobody recorded. The shape is a
// ratio of girth to height, so a missing height shifts it, not breaks it.
const referenceHeightCm = 170

// overallCap keeps the soft whole-body term from distorting the mesh.
const overallCap = 0.4

// Shape works out the figure's morph weights from a check and the person's
// height and BMI (zero when unknown). Each girth is read against height, so
// the same waist pushes a short figure further than a tall one.
func Shape(m Measurements, heightCm, bmi float64, base bodymap.Baseline) bodymap.Shape {
	if heightCm <= 0 {
		heightCm = referenceHeightCm
	}
	return bodymap.Shape{
		Waist:   girth(m.WaistCm, heightCm, base.Waist),
		Hips:    girth(m.HipsCm, heightCm, base.Hips),
		Chest:   girth(m.ChestCm, heightCm, base.Chest),
		Overall: overall(bmi),
	}
}

func girth(cm *float64, heightCm float64, g bodymap.Girth) float64 {
	if cm == nil || g.To <= g.From {
		return 0
	}
	return round3(clamp01((*cm/heightCm - g.From) / (g.To - g.From)))
}

// overall rises from a BMI of 22 and reaches its cap at 37. A soft term: the
// circumferences do the shaping, this only keeps a heavier figure from
// looking hollow where nothing was measured.
func overall(bmi float64) float64 {
	if bmi <= 0 {
		return 0
	}
	return round3(clamp01((bmi-22)/15) * overallCap)
}

func clamp01(v float64) float64 { return math.Max(0, math.Min(1, v)) }

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

// Summary is one line for the coach: the latest measurements, and how each
// moved since the oldest check given. checks are newest first.
func Summary(checks []Check) string {
	if len(checks) == 0 {
		return ""
	}
	latest := checks[0]
	var parts []string
	for _, f := range []struct {
		label string
		unit  string
		get   func(Check) *float64
	}{
		{"waist", "cm", func(c Check) *float64 { return c.WaistCm }},
		{"hips", "cm", func(c Check) *float64 { return c.HipsCm }},
		{"chest", "cm", func(c Check) *float64 { return c.ChestCm }},
		{"thigh", "cm", func(c Check) *float64 { return c.ThighCm }},
		{"arm", "cm", func(c Check) *float64 { return c.ArmCm }},
		{"weight", "kg", func(c Check) *float64 {
			if c.WeightCarried {
				return nil
			}
			return c.WeightKg
		}},
	} {
		now := f.get(latest)
		if now == nil {
			continue
		}
		part := fmt.Sprintf("%s %s %s", f.label, trim(*now), f.unit)
		if from, on, ok := oldest(checks[1:], f.get); ok {
			d := math.Round((*now-from)*10) / 10
			part += fmt.Sprintf(" (%s since %s)", signed(d), on.Format("2 Jan"))
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return ""
	}
	line := fmt.Sprintf("Progress check %s: %s.", latest.MeasuredOn.Format("2 Jan 2006"), strings.Join(parts, ", "))
	for _, c := range checks {
		if c.Source == SourceAssisted {
			line += " Some values began as photo estimates the person confirmed; treat them as approximate."
			break
		}
	}
	return line
}

// oldest is the earliest value a field has among checks (newest first).
func oldest(checks []Check, get func(Check) *float64) (float64, time.Time, bool) {
	for i := len(checks) - 1; i >= 0; i-- {
		if v := get(checks[i]); v != nil {
			return *v, checks[i].MeasuredOn, true
		}
	}
	return 0, time.Time{}, false
}

func trim(v float64) string {
	return strings.TrimSuffix(fmt.Sprintf("%.1f", v), ".0")
}

func signed(d float64) string {
	switch {
	case d > 0:
		return "+" + trim(d)
	case d < 0:
		return "−" + trim(-d)
	default:
		return "±0"
	}
}
