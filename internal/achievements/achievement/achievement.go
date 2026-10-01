// Package achievement holds the achievement types, apart from
// internal/achievements so web templates can name them without importing the
// service that renders them.
package achievement

import (
	"time"

	"github.com/google/uuid"
)

// Categories a person shares or keeps to themselves, one switch each.
const (
	CategoryTraining = "training"
	CategoryStreaks  = "streaks"
	CategoryGoals    = "goals"
)

// Categories in the order they are offered.
func Categories() []string { return []string{CategoryTraining, CategoryStreaks, CategoryGoals} }

// Kinds of moment, each in one category.
const (
	KindWorkoutCompleted = "workout_completed"
	KindStreakReached    = "streak_reached"
	KindGoalCompleted    = "goal_completed"
	KindMilestoneReached = "milestone_reached"
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
	Kudos       int
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
}
