// Package users owns the account record: who someone is, how they want to be
// coached, and what timezone their day runs in.
//
// Credentials and sessions live in internal/auth. This package deliberately
// knows nothing about passwords beyond storing an already-hashed value, so that
// the hashing policy has exactly one home.
package users

import (
	"strings"
	"time"

	"github.com/google/uuid"

	usersdb "github.com/NorthAIProject/north-client/internal/users/db"
)

// Tier decides which chain of AI providers serves a user. It is deliberately
// not a billing state: what North needs to know at coaching time is which
// backends to spend on, and whatever eventually manages subscriptions can set
// this without the coach learning anything about payments.
type Tier string

const (
	TierFree Tier = "free"
	TierPro  Tier = "pro"
)

// User is the domain view of an account. It excludes the password hash: nothing
// outside internal/auth has any business reading it, and leaving it off the
// type means it cannot be leaked into a template or a log line by accident.
type User struct {
	ID          uuid.UUID
	Email       string
	DisplayName string
	Timezone    string

	// CoachingStyle is the user's own description of how they want to be
	// coached. Empty until they set one.
	CoachingStyle string

	Tier Tier

	// OnboardedAt is set when the user completes or skips first-run onboarding.
	// Nil means the web app should show the onboarding flow once.
	OnboardedAt *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// NeedsOnboarding reports whether the web app should gate this account behind
// the first-run questionnaire.
func (u User) NeedsOnboarding() bool {
	return u.OnboardedAt == nil
}

// Location resolves the user's timezone, falling back to UTC. Scheduling a
// briefing must never fail because a stored timezone name became invalid.
func (u User) Location() *time.Location {
	loc, err := time.LoadLocation(u.Timezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// FirstName is the informal address the coach uses.
func (u User) FirstName() string {
	name := strings.TrimSpace(u.DisplayName)
	if name == "" {
		return "there"
	}
	if first, _, found := strings.Cut(name, " "); found {
		return first
	}
	return name
}

func fromDB(row usersdb.User) User {
	u := User{
		ID:          row.ID,
		Email:       row.Email,
		DisplayName: row.DisplayName,
		Timezone:    row.Timezone,
		Tier:        Tier(row.Tier),
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
	if row.CoachingStyle != nil {
		u.CoachingStyle = *row.CoachingStyle
	}
	u.OnboardedAt = row.OnboardedAt
	return u
}
