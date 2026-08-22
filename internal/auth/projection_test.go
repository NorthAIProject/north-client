package auth_test

import (
	"context"
	"testing"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/users"
)

// The session join builds a users.User by hand, because the row type belongs to
// this slice's generated package and internal/users cannot see it. Two
// hand-maintained projections of one table drift, and this one already had:
// it carried Tier but silently dropped CoachingTone, so the coach spoke in the
// default voice for every request that read the user from a session rather than
// from users.Service.
//
// Tier now decides both which provider chain serves an account and which quota
// ceiling applies, so a gap here stops being cosmetic and starts being a paying
// customer served the free plan.
func TestTheSessionUserMatchesTheStoredUser(t *testing.T) {
	svc, sessions, pool, _ := newService(t)
	ctx := context.Background()

	signedUp, _, err := svc.Signup(ctx, auth.SignupInput{
		Email:                "projection@north.test",
		DisplayName:          "Projection Check",
		Password:             goodPassword,
		PasswordConfirmation: goodPassword,
		Timezone:             "Europe/Lisbon",
	}, auth.Metadata{})
	if err != nil {
		t.Fatalf("signup: %v", err)
	}

	userSvc := users.NewService(users.NewRepository(pool))

	// Move every field the projection could forget away from its zero value, so
	// a dropped assignment shows up as a difference rather than as two zeroes
	// agreeing with each other.
	stored, err := userSvc.UpdateProfile(ctx, signedUp.ID, users.Profile{
		DisplayName:   "Projection Check",
		Timezone:      "Europe/Lisbon",
		CoachingStyle: "short answers, no preamble",
		CoachingTone:  users.ToneToughLove,
	})
	if err != nil {
		t.Fatalf("update profile: %v", err)
	}
	stored, err = userSvc.UpdateTier(ctx, stored.ID, users.TierPro)
	if err != nil {
		t.Fatalf("update tier: %v", err)
	}

	token, _, err := sessions.Create(ctx, stored.ID, auth.Metadata{})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	session, err := sessions.Resolve(ctx, token)
	if err != nil {
		t.Fatalf("resolve session: %v", err)
	}

	got := session.User

	if got.Tier != stored.Tier {
		t.Errorf("Tier = %q, want %q", got.Tier, stored.Tier)
	}
	if got.CoachingTone != stored.CoachingTone {
		t.Errorf("CoachingTone = %q, want %q", got.CoachingTone, stored.CoachingTone)
	}
	if got.CoachingStyle != stored.CoachingStyle {
		t.Errorf("CoachingStyle = %q, want %q", got.CoachingStyle, stored.CoachingStyle)
	}
	if got.DisplayName != stored.DisplayName {
		t.Errorf("DisplayName = %q, want %q", got.DisplayName, stored.DisplayName)
	}
	if got.Timezone != stored.Timezone {
		t.Errorf("Timezone = %q, want %q", got.Timezone, stored.Timezone)
	}
	if got.Email != stored.Email {
		t.Errorf("Email = %q, want %q", got.Email, stored.Email)
	}
	if got.ID != stored.ID {
		t.Errorf("ID = %v, want %v", got.ID, stored.ID)
	}
	if (got.OnboardedAt == nil) != (stored.OnboardedAt == nil) {
		t.Errorf("OnboardedAt presence = %v, want %v", got.OnboardedAt != nil, stored.OnboardedAt != nil)
	}
}
