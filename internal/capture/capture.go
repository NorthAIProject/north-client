// Package capture turns one line of ordinary text into the day's logs.
//
// It owns no tables. Every value it produces is written through the slice that
// already owns it — hydration, sleep, habits, biometrics, meals, check-ins —
// which is why this package is composition in the style of internal/care and
// internal/dashboard rather than a seventh place a weight can live.
//
// The shape of the work is deliberate: parsing and writing are two steps with a
// person in between. internal/coach/confirm.go shows an approval card for every
// tool call that writes, and routing a sentence with four logs in it through
// the chat loop would put four cards in front of one thought. Here the preview
// is the card, and the single Save is the approval.
//
// The invariant that keeps that honest, and which every change here has to
// preserve: nothing in this package writes without a POST a person made after
// seeing the rendered items. Parse costs a model call and changes no row;
// Commit changes rows and costs nothing.
package capture

import "github.com/NorthAIProject/north-client/internal/capture/captured"

// The captured shapes, re-exported so callers name one package.
type (
	Kind    = captured.Kind
	Item    = captured.Item
	Water   = captured.Water
	Sleep   = captured.Sleep
	Habit   = captured.Habit
	Weight  = captured.Weight
	CheckIn = captured.CheckIn
	Food    = captured.Food
	Draft   = captured.Draft
	Outcome = captured.Outcome
	Receipt = captured.Receipt
)

const (
	KindWater   = captured.KindWater
	KindSleep   = captured.KindSleep
	KindHabit   = captured.KindHabit
	KindWeight  = captured.KindWeight
	KindCheckIn = captured.KindCheckIn
	KindFood    = captured.KindFood

	MaxText  = captured.MaxText
	MaxItems = captured.MaxItems
)

var (
	Kinds       = captured.Kinds
	Validate    = captured.Validate
	ValidateAll = captured.ValidateAll
	Writable    = captured.Writable
	Summary     = captured.Summary
)
