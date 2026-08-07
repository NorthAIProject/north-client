// Package hydration tracks water intake.
//
// Drinks are recorded one at a time and summed per local day, rather than
// stored as a single editable daily total: people log as they drink, and a
// running total that can be typed over loses the timestamps that make
// "you drink nothing until 4pm" visible.
package hydration

import "github.com/NorthAIProject/north-client/internal/hydration/hydration"

// The intake shapes live in a leaf package so the service and any template
// that renders them do not import each other.
type (
	Entry = hydration.Entry
	Day   = hydration.Day
)

const (
	Glass  = hydration.Glass
	Bottle = hydration.Bottle
	Litre  = hydration.Litre

	DefaultDailyTargetML = hydration.DefaultDailyTargetML
)

var QuickAmounts = hydration.QuickAmounts
