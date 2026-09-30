package util

import "math"

// RoundUpToIncrement rounds value up to the nearest multiple of increment. A non-positive
// increment means no rounding is configured, so the value passes through untouched.
func RoundUpToIncrement(value float64, increment int) float64 {
	if increment <= 0 {
		return value
	}
	inc := float64(increment)
	return math.Ceil(value/inc) * inc
}

// RoundHalfUpToScale rounds value half-away-from-zero to the given number of decimal places.
func RoundHalfUpToScale(value float64, scale int) float64 {
	if scale < 0 {
		return value
	}
	factor := math.Pow(10, float64(scale))
	return math.Round(value*factor) / factor
}
