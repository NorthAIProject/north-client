package sync

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Nudges manages proactive reminders and in-app alerts.
type Nudges interface {
	DismissByKind(ctx context.Context, userID uuid.UUID, kind, dedupeKey, updateText string) error
}

// Coordinator synchronizes domain events (workout completion, check-in, meal logs)
// across client platforms (iOS, Telegram, Web).
type Coordinator struct {
	nudges Nudges
}

// NewCoordinator creates a new multi-client event sync coordinator.
func NewCoordinator(nudges Nudges) *Coordinator {
	return &Coordinator{nudges: nudges}
}

// OnWorkoutCompleted resolves today's workout reminders and updates messaging channels.
func (c *Coordinator) OnWorkoutCompleted(ctx context.Context, userID uuid.UUID, title string) error {
	today := time.Now().Format("2006-01-02")
	updateText := "✅ Completed today's session"
	if title != "" {
		updateText = fmt.Sprintf("✅ Completed today's session: %s", title)
	}
	if c.nudges != nil {
		_ = c.nudges.DismissByKind(ctx, userID, "workout_today", today, updateText)
	}
	return nil
}

// OnCheckInSaved resolves missed check-in and streak warning nudges.
func (c *Coordinator) OnCheckInSaved(ctx context.Context, userID uuid.UUID) error {
	today := time.Now().Format("2006-01-02")
	if c.nudges != nil {
		_ = c.nudges.DismissByKind(ctx, userID, "missed_checkin", today, "✅ Checked in today")
		_ = c.nudges.DismissByKind(ctx, userID, "streak_at_risk", today, "✅ Checked in today")
	}
	return nil
}

// OnMealLogged is called when a meal is recorded.
func (c *Coordinator) OnMealLogged(ctx context.Context, userID uuid.UUID, mealName string) error {
	return nil
}
