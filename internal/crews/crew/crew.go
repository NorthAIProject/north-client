// Package crew holds the crew types, apart from internal/crews so templates
// can name them without importing the service.
package crew

import (
	"time"

	"github.com/google/uuid"
)

// Challenge kinds: so many check-ins, or so many workouts, a week.
const (
	ChallengeCheckIns = "checkins"
	ChallengeWorkouts = "workouts"
)

// Size limits, by the owner's tier: a crew is small on purpose, and Pro lets
// a club or a team use one.
const (
	MaxMembers    = 8
	MaxMembersPro = 50
)

type Crew struct {
	ID      uuid.UUID
	Name    string
	OwnerID uuid.UUID
	Code    string
	Members int
}

type Challenge struct {
	Kind   string
	Target int
}

// Member is one row of the crew board: today, the streak, and this week.
type Member struct {
	ID           uuid.UUID
	DisplayName  string
	Handle       string
	CheckedIn    bool
	WorkedOut    bool
	Streak       int
	WeekProgress int
	Owner        bool
	Me           bool
}

// Board is a crew as a member sees it.
type Board struct {
	Crew
	Challenge *Challenge
	Members   []Member
	// WeekStart is the Monday the challenge counts from, in the viewer's zone.
	WeekStart time.Time
	IsOwner   bool
}
