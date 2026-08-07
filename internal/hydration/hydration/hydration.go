// Package hydration holds the shape of a water-intake entry and a day's
// total.
//
// A leaf, so the hydration service and any template that renders intake do
// not import each other. See CLAUDE.md on slice layout.
package hydration

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Common pour sizes, offered as one-tap buttons. Millilitres throughout: the
// preferences slice owns unit display, and storing one unit means a person
// who switches systems does not orphan their history.
const (
	Glass  = 250
	Bottle = 500
	Litre  = 1000
)

// QuickAmounts is the ordered set offered in the UI.
var QuickAmounts = []int{Glass, Bottle, Litre}

// DefaultDailyTargetML is what a day is measured against until the product
// learns something better. Deliberately a flat number rather than a formula
// off body weight: the honest version depends on climate, activity and diet,
// and a precise-looking wrong number is worse than a round approximate one.
const DefaultDailyTargetML = 2000

// Entry is one recorded drink.
type Entry struct {
	ID     uuid.UUID
	UserID uuid.UUID

	// LogDate is the user's local calendar day, so a late-night glass counts
	// toward the day they think they drank it.
	LogDate  time.Time
	AmountML int

	LoggedAt time.Time
}

// Day is one calendar day's intake, aggregated from its entries.
type Day struct {
	Date     time.Time
	TotalML  int
	Entries  int
	TargetML int
}

// Percent is progress toward the day's target, capped at 100 so a bar cannot
// overflow. Zero target means unset rather than "impossible", so report 0.
func (d Day) Percent() int {
	if d.TargetML <= 0 {
		return 0
	}
	pct := d.TotalML * 100 / d.TargetML
	if pct > 100 {
		return 100
	}
	return pct
}

// Summary renders a day for the coach's context.
func (d Day) Summary() string {
	if d.TotalML == 0 {
		return "Water today: nothing logged"
	}
	return fmt.Sprintf("Water today: %.1fL of a %.1fL target (%d%%), across %d drinks",
		float64(d.TotalML)/1000, float64(d.TargetML)/1000, d.Percent(), d.Entries)
}
