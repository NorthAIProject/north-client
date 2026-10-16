// Package outreach is the coach speaking first: once a day at most, when it
// notices something in the person's day, it acts or offers and says so.
//
// Two triggers so far. On a low-recovery morning with a session due, the
// coach lightens it (or, if the person keeps that to themselves, offers to)
// and says why, with an undo. The morning after a missed session, it offers
// to move the session to today or tomorrow, or to let it go.
//
// Each outreach is one row a day per person (coach_outreach), so the limit
// holds across sweeps and channels, and the answer recorded on it is how we
// learn whether anyone answers.
package outreach

import (
	"time"

	"github.com/google/uuid"
)

// Triggers.
const (
	TriggerLowRecovery   = "low_recovery"
	TriggerMissedSession = "missed_session"
)

// Actions, the answers a person can give. They travel as button values, so
// they are part of the wire format.
const (
	ActionUndo     = "undo"
	ActionLighter  = "lighter"
	ActionKeep     = "keep"
	ActionToday    = "today"
	ActionTomorrow = "tomorrow"
	ActionLetGo    = "let_go"
)

// Facts are what the coach acted on or offered.
type Facts struct {
	// Session is the plan session concerned: today's, or yesterday's missed.
	Session string `json:"session"`

	// Applied is a low morning the coach already lightened itself.
	Applied bool `json:"applied,omitempty"`
	// Reason is what is off its usual, in words, on a low morning.
	Reason string `json:"reason,omitempty"`

	// Weekday is the missed session's day; TodayFree and TomorrowFree say
	// where it could move.
	Weekday      string `json:"weekday,omitempty"`
	TodayFree    bool   `json:"todayFree,omitempty"`
	TomorrowFree bool   `json:"tomorrowFree,omitempty"`
}

// Outreach is one day's message.
type Outreach struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Day       time.Time
	Trigger   string
	Facts     Facts
	Body      string
	MessageID *uuid.UUID
	Action    string
	ActedAt   *time.Time
	CreatedAt time.Time
}

// Actions are the answers this outreach offers, in the order they show.
func (o Outreach) Actions() []string {
	switch o.Trigger {
	case TriggerLowRecovery:
		if o.Facts.Applied {
			return []string{ActionUndo}
		}
		return []string{ActionLighter, ActionKeep}
	case TriggerMissedSession:
		var out []string
		if o.Facts.TodayFree {
			out = append(out, ActionToday)
		}
		if o.Facts.TomorrowFree {
			out = append(out, ActionTomorrow)
		}
		return append(out, ActionLetGo)
	default:
		return nil
	}
}

// Offers reports whether action is one of this outreach's answers.
func (o Outreach) Offers(action string) bool {
	for _, a := range o.Actions() {
		if a == action {
			return true
		}
	}
	return false
}

// Answered reports whether the person has answered.
func (o Outreach) Answered() bool { return o.Action != "" }

// NudgeKind is the notification kind this outreach is raised as: it picks
// the push category, and so the buttons under the banner.
func (o Outreach) NudgeKind() string {
	if o.Trigger == TriggerLowRecovery {
		return "coach_lighter"
	}
	return "coach_missed"
}
