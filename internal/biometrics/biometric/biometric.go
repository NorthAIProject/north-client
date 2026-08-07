// Package biometric holds the shape of a person's body measurements.
//
// A leaf, so the biometrics service and anything that renders a measurement
// can both import it without importing each other. See CLAUDE.md on slice
// layout.
package biometric

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Sex values a biometric record can hold. Used by the Mifflin-St Jeor BMR
// formula, which applies a different constant for each.
const (
	SexMale   = "male"
	SexFemale = "female"
)

// Sexes is the set offered in the UI.
var Sexes = []string{SexMale, SexFemale}

// Biometric is one measurement of a person's body, in metric units.
//
// Weight and height are stored per-measurement rather than as a single
// mutable value on the user, because a body changes and the coach benefits
// from seeing the trend, not just the latest number.
type Biometric struct {
	ID     uuid.UUID
	UserID uuid.UUID

	WeightKg    float64
	HeightCm    float64
	DateOfBirth time.Time
	Sex         string

	// IsCurrent marks the most recent measurement. Older rows are kept for
	// history but are never read by the calculator or activity tracker.
	IsCurrent bool

	CreatedAt time.Time
}

// AgeYears is the person's age at the given instant, in whole years.
func (b Biometric) AgeYears(at time.Time) int {
	years := at.Year() - b.DateOfBirth.Year()
	// Subtract one if the birthday has not happened yet this year.
	birthdayThisYear := time.Date(at.Year(), b.DateOfBirth.Month(), b.DateOfBirth.Day(), 0, 0, 0, 0, at.Location())
	if at.Before(birthdayThisYear) {
		years--
	}
	return years
}

// HeightMeters converts the stored centimetres to metres, for BMI.
func (b Biometric) HeightMeters() float64 { return b.HeightCm / 100 }

// BMI is weight in kilograms divided by height in metres squared.
func (b Biometric) BMI() float64 {
	m := b.HeightMeters()
	return b.WeightKg / (m * m)
}

// Summary renders a biometric record for the coach's context.
func (b Biometric) Summary() string {
	return fmt.Sprintf("%.1fkg, %.0fcm, %d years old, BMI %.1f", b.WeightKg, b.HeightCm, b.AgeYears(b.CreatedAt), b.BMI())
}
