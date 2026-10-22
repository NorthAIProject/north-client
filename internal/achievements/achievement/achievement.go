// Package achievement holds the achievement types, apart from
// internal/achievements so web templates can name them without importing the
// service that renders them.
package achievement

import (
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/lifts/lift"
)

// Categories a person shares or keeps to themselves, one switch each.
const (
	CategoryTraining = "training"
	CategoryStreaks  = "streaks"
	CategoryGoals    = "goals"
	// CategoryXP is XP and level, which is what puts a person on a friend's
	// leaderboard. No achievement is ever in it.
	CategoryXP = "xp"
	// CategoryAwards is the badges a person earns. On by default, like the
	// first three; a person can switch it off.
	CategoryAwards = "awards"
)

// Categories in the order they are offered.
func Categories() []string {
	return []string{CategoryTraining, CategoryStreaks, CategoryGoals, CategoryXP, CategoryAwards}
}

// DefaultShared is whether a category is shared with followers by a person who
// never stored a choice. It mirrors the SQL function sharing_on.
func DefaultShared(category string) bool {
	switch category {
	case CategoryTraining, CategoryStreaks, CategoryGoals, CategoryAwards:
		return true
	}
	return false
}

// Kinds of moment, each in one category.
const (
	KindWorkoutCompleted = "workout_completed"
	KindStreakReached    = "streak_reached"
	KindGoalCompleted    = "goal_completed"
	KindMilestoneReached = "milestone_reached"
	KindWeekReviewed     = "week_reviewed"
	KindAreaStreak       = "area_streak"
	KindCrewChallengeMet = "crew_challenge_met"
	// KindRecapShared is a 7- or 30-day recap the person posted on purpose.
	// It sits in training: the feed's category is a closed list in the
	// shipped app, and a recap is mostly what was trained.
	KindRecapShared = "recap_shared"
	// KindBadgeEarned is a badge from internal/awards/award; the source key
	// is the badge id.
	KindBadgeEarned = "badge_earned"
)

// Item is one achievement in a feed.
type Item struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	DisplayName string
	Handle      string
	Category    string
	Kind        string
	Title       string
	Detail      string
	OccurredAt  time.Time
	// SourceKey is what the moment is about: the session id for a workout.
	SourceKey string
	// Workout is the finished workout's card, set on a workout_completed
	// item when it could be built.
	Workout *lift.Card
	Kudos   int
	// Kudoed is whether the viewer gave kudos.
	Kudoed bool
	// Mine is whether the viewer is its owner.
	Mine bool
}

// Sharing is which categories followers may see.
type Sharing struct {
	Training bool
	Streaks  bool
	Goals    bool
	XP       bool
	Awards   bool
}
