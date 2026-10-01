// Package durfmt formats durations the way more than one page needs them.
package durfmt

import "fmt"

// HoursMinutes renders whole minutes as "1h 05m", always with both parts and
// the minutes padded, so figures line up when shown in a row or a sentence.
// Short durations still read "0h 45m"; pages that want "45m" format their own.
func HoursMinutes(minutes int) string {
	return fmt.Sprintf("%dh %02dm", minutes/60, minutes%60)
}
