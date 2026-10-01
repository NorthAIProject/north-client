package sync

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/nudges"
)

// Nudges manages proactive reminders and in-app alerts.
type Nudges interface {
	// ResolveToday closes today's nudges of kind, in the person's own time
	// zone, and rewrites each chat message that carried one with
	// text(title, body) of that nudge.
	ResolveToday(ctx context.Context, userID uuid.UUID, kind string, text func(title, body string) string) error
}

// Coordinator synchronizes domain events (workout completion, check-in, meal logs)
// across client platforms (iOS, Telegram, Web).
//
// Every failure is logged here: the event itself already happened, so callers
// never fail on a reminder that could not be tidied up.
type Coordinator struct {
	nudges Nudges
	log    *slog.Logger
}

// NewCoordinator creates a new multi-client event sync coordinator.
func NewCoordinator(nudges Nudges) *Coordinator {
	return &Coordinator{nudges: nudges, log: slog.Default()}
}

// OnWorkoutCompleted resolves today's workout reminder. The message names the
// plan day the reminder named ("Upper A"); title is the fallback when the
// reminder had none.
func (c *Coordinator) OnWorkoutCompleted(ctx context.Context, userID uuid.UUID, title string) error {
	return c.resolve(ctx, userID, nudges.KindWorkoutToday, func(_, body string) string {
		if body == "" {
			body = title
		}
		if body == "" {
			return "✅ Completed today's session"
		}
		return "✅ Completed today's session: " + body
	})
}

// OnCheckInSaved resolves missed check-in and streak warning nudges.
func (c *Coordinator) OnCheckInSaved(ctx context.Context, userID uuid.UUID) error {
	checkedIn := func(_, _ string) string { return "✅ Checked in today" }
	return errors.Join(
		c.resolve(ctx, userID, nudges.KindMissedCheckIn, checkedIn),
		c.resolve(ctx, userID, nudges.KindStreakAtRisk, checkedIn),
	)
}

// OnMealLogged resolves the meal reminders raised so far today. Any meal
// answers them: a reminder asks to log what was eaten, not a particular dish.
func (c *Coordinator) OnMealLogged(ctx context.Context, userID uuid.UUID) error {
	return c.resolve(ctx, userID, nudges.KindMealReminder, func(title, _ string) string {
		return "✅ Done: " + title
	})
}

func (c *Coordinator) resolve(ctx context.Context, userID uuid.UUID, kind string, text func(title, body string) string) error {
	if c.nudges == nil {
		return nil
	}
	err := c.nudges.ResolveToday(ctx, userID, kind, text)
	if err != nil {
		c.log.Warn("sync could not resolve nudges", "error", err, "user_id", userID, "kind", kind)
	}
	return err
}
