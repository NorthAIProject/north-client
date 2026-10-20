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

// Role is what a person may do in a crew. The owner is crews.owner_id, the
// one place ownership lives; every other member is an admin or not.
type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

// CanManage is true for those who run the crew: the challenge, channels,
// pins, and removing plain members.
func (r Role) CanManage() bool { return r == RoleOwner || r == RoleAdmin }

// Channel kinds: text to talk in, board for posts with threads, and wins,
// where the achievements members already share land.
const (
	ChannelText  = "text"
	ChannelBoard = "board"
	ChannelWins  = "wins"
)

type Channel struct {
	ID        uuid.UUID
	Kind      string
	Name      string
	Topic     string
	Position  int
	IsDefault bool
}

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
	Role         Role
}

// Board is a crew as a member sees it.
type Board struct {
	Crew
	Challenge *Challenge
	Members   []Member
	// WeekStart is the Monday the challenge counts from, in the viewer's zone.
	WeekStart time.Time
	IsOwner   bool
	MyRole    Role
}
