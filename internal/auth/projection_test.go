package auth_test

import (
	"context"
	"reflect"
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
		Locale:        users.LocalePTBR,
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

	// Compared field by field through reflection rather than by a list of
	// checks somebody has to remember to extend.
	//
	// The list was the problem. This projection has now silently dropped three
	// fields in turn — Tier, then CoachingTone, then Locale — each time because
	// a field was added to users.User and to users.fromDB and not to this one,
	// and each time the test passed because it did not know to look. Every
	// field above has been moved away from its zero value, so any assignment
	// missing from userFromDB shows up here as a difference.
	storedValue, gotValue := reflect.ValueOf(stored), reflect.ValueOf(got)
	for i := range storedValue.NumField() {
		field := storedValue.Type().Field(i)
		want, have := storedValue.Field(i).Interface(), gotValue.Field(i).Interface()

		// OnboardedAt is a pointer to a time, and a signup that has not been
		// through onboarding leaves it nil on both sides. Presence is the only
		// thing worth comparing.
		if field.Name == "OnboardedAt" {
			if (stored.OnboardedAt == nil) != (got.OnboardedAt == nil) {
				t.Errorf("OnboardedAt presence = %v, want %v", got.OnboardedAt != nil, stored.OnboardedAt != nil)
			}
			continue
		}

		if !reflect.DeepEqual(want, have) {
			t.Errorf("%s = %v, want %v — add it to auth.userFromDB", field.Name, have, want)
		}

		// A field still at its zero value proves nothing: two zeroes agree with
		// each other whether or not the projection assigns anything.
		if reflect.DeepEqual(want, reflect.Zero(field.Type).Interface()) {
			t.Errorf("%s is still at its zero value, so this check cannot fail — set it above", field.Name)
		}
	}
}
