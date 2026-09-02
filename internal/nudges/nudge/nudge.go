// Package nudge holds the shape of one in-app coach nudge.
package nudge

import (
	"time"

	"github.com/google/uuid"
)

const (
	KindMissedCheckIn     = "missed_checkin"
	KindStreakAtRisk      = "streak_at_risk"
	KindGoalDeadline      = "goal_deadline"
	KindFirstWeekCheck    = "first_week_check"
	KindFirstWeekEvidence = "first_week_evidence"
	KindFirstWeekReview   = "first_week_review"
	KindWorkoutToday      = "workout_today"
	KindFormReady         = "form_ready"
	KindCoachReply        = "coach_reply"
	KindBriefingReady     = "briefing_ready"
	KindPhotoAsk          = "photo_ask"
	KindPhotoReminder     = "photo_reminder"
)

// Nudge is one scheduled accountability note for a person.
type Nudge struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	Kind        string
	DedupeKey   string
	Title       string
	Body        string
	Href        string
	ReadAt      *time.Time
	DismissedAt *time.Time
	CreatedAt   time.Time
}

// Unread reports whether the person has not yet opened this nudge.
func (n Nudge) Unread() bool {
	return n.ReadAt == nil && n.DismissedAt == nil
}
