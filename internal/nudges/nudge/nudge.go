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
	// KindFirstWeekEvening is the evening note on a first-week day whose
	// action is still open. Replaces the three notes above while a programme
	// (internal/firstweek) runs.
	KindFirstWeekEvening = "first_week_evening"
	KindWorkoutToday     = "workout_today"
	KindFormReady        = "form_ready"
	KindCoachReply       = "coach_reply"
	KindBriefingReady    = "briefing_ready"
	KindPhotoAsk         = "photo_ask"
	KindPhotoReminder    = "photo_reminder"
	KindMealReminder     = "meal_reminder"
	// KindEveningReflection is the nightly "how did today go", opted into
	// separately from the accountability nudges.
	KindEveningReflection = "evening_reflection"
	// KindCrewCheckIn is the evening note that crewmates have checked in
	// and you have not.
	KindCrewCheckIn = "crew_checkin"

	// KindWeekReview asks on Sunday evening for the weekly review: how the
	// week went, and the focus for the next one.
	KindWeekReview = "week_review"

	// KindDecisionRevisit asks 30 and 90 days after a decision whether it
	// held.
	KindDecisionRevisit = "decision_revisit"

	// KindMedicationReminder is a scheduled dose whose time has come. It is
	// the alarm the person set, so quiet hours do not hold it back.
	KindMedicationReminder = "medication_reminder"

	// KindProgressCheckDue is the progress check the person scheduled
	// (weekly, every two weeks, monthly) falling due.
	KindProgressCheckDue = "progress_check_due"
	// KindCoachLighter is the coach saying it lightened today's session on a
	// low-recovery morning (or offering to), with an undo. Raised by
	// internal/outreach, never by the nudge sweep.
	KindCoachLighter = "coach_lighter"
	// KindCoachMissed is the coach offering to move a session missed
	// yesterday, or to let it go. Raised by internal/outreach.
	KindCoachMissed = "coach_missed"
	// KindCoachFirstWeek is the first week's morning message, with a Done
	// button. Raised by internal/outreach.
	KindCoachFirstWeek = "coach_first_week"
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
