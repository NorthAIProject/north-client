// Package screen holds a day's screen time.
package screen

import (
	"fmt"
	"time"
)

// DailyLimitMinutes is the budget the gauge fills against: four hours, a
// common "healthy" figure for adults outside work.
const DailyLimitMinutes = 240

// Day is one day's screen time.
type Day struct {
	LocalDate time.Time
	Minutes   int
	// Source is "manual" or "shortcut".
	Source string
}

// Summary renders a day for the coach.
func (d Day) Summary() string {
	return fmt.Sprintf("Screen time %s: %dh %dm.", d.LocalDate.Format("2 Jan"), d.Minutes/60, d.Minutes%60)
}

// ValidSource reports whether s is a source this build knows.
func ValidSource(s string) bool { return s == "manual" || s == "shortcut" }
