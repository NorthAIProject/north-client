// Package biometrics owns a person's body measurements: weight, height, date
// of birth, and sex. These are the inputs the calculator and activity tracker
// need and nothing narrates them to the coach directly — they exist to feed
// other services, not to be a conversation topic on their own.
package biometrics

import "github.com/NorthAIProject/north-client/internal/biometrics/biometric"

// The biometric shape lives in a leaf package so the service and any future
// template that renders one do not import each other.
type Biometric = biometric.Biometric

const (
	SexMale   = biometric.SexMale
	SexFemale = biometric.SexFemale
)

var Sexes = biometric.Sexes
