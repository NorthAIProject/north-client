// Package outreach is the coach speaking first: once a day at most, when it
// notices something in the person's day, it acts or offers and says so.
//
// Three triggers. During a new person's first week (internal/firstweek), the
// morning message is the week's: yesterday, and today's one action, with a
// Done button. The other two stay quiet that week; what they would have said
// goes into the first-week message instead. Otherwise: on a low-recovery morning with a session due, the
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
	TriggerFirstWeek     = "first_week"
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
	ActionDone     = "done"
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

	// The first week: the programme day, its template, and whether
	// yesterday's action was done (Day 1 has no yesterday in the week).
	// Reason and Session carry what the low-recovery and missed-session
	// triggers noticed, since those stay quiet that week.
	FirstWeekDay  int    `json:"firstWeekDay,omitempty"`
	Template      string `json:"template,omitempty"`
	YesterdayDone bool   `json:"yesterdayDone,omitempty"`
	Missed        string `json:"missed,omitempty"`

	// Review is day 7's message, the week looked back on: the days done,
	// and the days the template's log turned up (absent when it has none).
	Review     bool `json:"review,omitempty"`
	DaysDone   int  `json:"daysDone,omitempty"`
	LoggedDays *int `json:"loggedDays,omitempty"`
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
	case TriggerFirstWeek:
		return []string{ActionDone}
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

// reviewHref is the first week's review page; firstweek owns the route.
const reviewHref = "/app/first-week/review"

// Href is where the notification opens: the review on day 7, My Day
// otherwise.
func (o Outreach) Href() string {
	if o.Trigger == TriggerFirstWeek && o.Facts.Review {
		return reviewHref
	}
	return "/app"
}

// NudgeKind is the notification kind this outreach is raised as: it picks
// the push category, and so the buttons under the banner.
func (o Outreach) NudgeKind() string {
	switch o.Trigger {
	case TriggerLowRecovery:
		return "coach_lighter"
	case TriggerFirstWeek:
		return "coach_first_week"
	default:
		return "coach_missed"
	}
}
