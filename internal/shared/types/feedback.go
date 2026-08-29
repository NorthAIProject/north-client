package types

import "fmt"

// The three answers a person can give to "did this help?".
//
// The vocabulary lives here rather than in either handler because two slices
// accept it — coach replies and reports — and a vocabulary defined twice is one
// that drifts the moment a third caller appears. "up"/"down" was the obvious
// alternative and was rejected: those name the widget, and the widget is the
// thing most likely to change.
const (
	HelpfulYes   = "helpful"
	HelpfulNo    = "unhelpful"
	HelpfulClear = "clear"
)

// ParseHelpful turns a submitted answer into the three-state value the column
// holds: true, false, or nil for "they took it back".
//
// Clearing is a real answer rather than an omission. Somebody who taps the wrong
// thumb needs a way out, and without one the only correction available is a
// wrong label that stays wrong — in the one column the product will later train
// judgement on.
func ParseHelpful(value string) (*bool, error) {
	switch value {
	case HelpfulYes:
		yes := true
		return &yes, nil
	case HelpfulNo:
		no := false
		return &no, nil
	case HelpfulClear:
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown feedback answer %q; want %s, %s, or %s",
			value, HelpfulYes, HelpfulNo, HelpfulClear)
	}
}
