// Package nudges owns scheduled in-app coach accountability: missed check-ins
// and approaching goal deadlines. The worker evaluates the rules; the web
// process only lists, marks read, and dismisses.
package nudges

import "github.com/NorthAIProject/north-client/internal/nudges/nudge"

type Nudge = nudge.Nudge

const (
	KindMissedCheckIn     = nudge.KindMissedCheckIn
	KindGoalDeadline      = nudge.KindGoalDeadline
	KindFirstWeekCheck    = nudge.KindFirstWeekCheck
	KindFirstWeekEvidence = nudge.KindFirstWeekEvidence
	KindFirstWeekReview   = nudge.KindFirstWeekReview
	KindWorkoutToday      = nudge.KindWorkoutToday
	KindFormReady         = nudge.KindFormReady
	KindCoachReply        = nudge.KindCoachReply
	KindBriefingReady     = nudge.KindBriefingReady
	KindPhotoAsk          = nudge.KindPhotoAsk
	KindPhotoReminder     = nudge.KindPhotoReminder
)
