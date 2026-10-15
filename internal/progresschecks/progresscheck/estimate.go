package progresscheck

import (
	"math"
	"strings"
)

// Estimate is a model's reading of tape measurements off the photos. It is
// never saved as is: it fills the form, and the person saves what they accept,
// marked assisted.
//
// A nil measurement is one the photos did not show well enough to estimate.
type Estimate struct {
	WaistCm *float64
	HipsCm  *float64
	ChestCm *float64

	// Confidence is low, medium or high.
	Confidence string

	// Notes is one short line on what limited the estimate.
	Notes string
}

// maxEstimateNotes bounds the notes a model writes. Under the column's 280 so
// a note survives the save untouched.
const maxEstimateNotes = 200

// Empty reports an estimate with no measurement at all.
func (e Estimate) Empty() bool {
	return e.WaistCm == nil && e.HipsCm == nil && e.ChestCm == nil
}

// Clean makes a model's reply safe to put in front of the person.
//
// A value outside the range a save would accept is dropped rather than
// clamped: it means the model misread the photo, and a clamped number would
// look like a measurement. Zero is the schema's "not visible", so it is
// dropped the same way. Unknown confidence reads as low.
func (e Estimate) Clean() Estimate {
	e.WaistCm = plausible(e.WaistCm, "waist_cm")
	e.HipsCm = plausible(e.HipsCm, "hips_cm")
	e.ChestCm = plausible(e.ChestCm, "chest_cm")

	e.Confidence = strings.ToLower(strings.TrimSpace(e.Confidence))
	if !validConfidence(e.Confidence) || e.Empty() {
		e.Confidence = "low"
	}

	e.Notes = strings.Join(strings.Fields(e.Notes), " ")
	if r := []rune(e.Notes); len(r) > maxEstimateNotes {
		e.Notes = strings.TrimSpace(string(r[:maxEstimateNotes-1])) + "…"
	}
	return e
}

func plausible(v *float64, field string) *float64 {
	if v == nil || math.IsNaN(*v) {
		return nil
	}
	for _, b := range bounds {
		if b.field != field {
			continue
		}
		if *v < b.min || *v > b.max {
			return nil
		}
		rounded := math.Round(*v*10) / 10
		return &rounded
	}
	return nil
}
