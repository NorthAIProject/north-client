package reports

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/insights"
	"github.com/NorthAIProject/north-client/internal/users"
)

// The morning briefing's view of the day ahead: how recovered the body looks,
// what training is planned, and what the calendar holds. The weekly review has
// no use for any of it, so only the daily briefing reads it.

// Recovery reads today's recovery: the one rule for whether a morning is low,
// shared with the lighter-day offer, the Progress screen and the coach.
// insights.RecoverySource satisfies it.
type Recovery interface {
	Recovery(ctx context.Context, user users.User, now time.Time) (insights.RecoveryData, error)
}

type SessionReader interface {
	DueToday(ctx context.Context, user users.User, today time.Time) (string, string, bool, error)
	CompletedToday(ctx context.Context, user users.User, today time.Time) (bool, string, error)
}

type CalendarReader interface {
	Upcoming(ctx context.Context, userID uuid.UUID) ([]string, error)
}

// TodayContext loads the "Today" section. Any reader may be nil, and a reader
// that fails costs only its own lines: a calendar that cannot be reached must
// not cost somebody their briefing.
type TodayContext struct {
	recovery Recovery
	sessions SessionReader
	calendar CalendarReader
}

func NewTodayContext(r Recovery, s SessionReader, c CalendarReader) *TodayContext {
	return &TodayContext{recovery: r, sessions: s, calendar: c}
}

// maxCalendarLines keeps the briefing about today rather than a week's diary.
const maxCalendarLines = 6

func (t *TodayContext) Load(ctx context.Context, user users.User, now time.Time) []string {
	var lines []string
	if t.recovery != nil {
		lines = append(lines, t.readiness(ctx, user, now)...)
	}
	if t.sessions != nil {
		lines = append(lines, t.session(ctx, user, now)...)
	}
	if t.calendar != nil {
		if upcoming, err := t.calendar.Upcoming(ctx, user.ID); err == nil {
			for i, line := range upcoming {
				if i == maxCalendarLines {
					break
				}
				lines = append(lines, "Calendar: "+line)
			}
		}
	}
	return lines
}

// readiness is the verdict the briefing may repeat. It is decided from
// numbers, so the model never has to judge recovery on its own, and by the
// same rule as the lighter-day offer, so the two never disagree.
func (t *TodayContext) readiness(ctx context.Context, user users.User, now time.Time) []string {
	r, err := t.recovery.Recovery(ctx, user, now)
	if err != nil {
		return nil
	}
	line, ok := r.Sentence()
	if !ok {
		return nil
	}
	if r.Low() {
		return []string{line, "Readiness: LOW — recovery is under this person's usual this morning."}
	}
	return []string{line, "Readiness: normal."}
}

func (t *TodayContext) session(ctx context.Context, user users.User, now time.Time) []string {
	today := now.In(user.Location())
	title, _, due, err := t.sessions.DueToday(ctx, user, today)
	if err != nil || !due {
		return []string{"Training: nothing scheduled today."}
	}
	done, _, err := t.sessions.CompletedToday(ctx, user, today)
	if err == nil && done {
		return []string{fmt.Sprintf("Training: today's session (%s) is already done.", title)}
	}
	return []string{fmt.Sprintf("Training: %s is scheduled today.", title)}
}
