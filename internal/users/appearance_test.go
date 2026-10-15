package users_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/theme"
	"github.com/NorthAIProject/north-client/internal/users"
)

// recorded is one appearance_changed the service reported.
type recorded struct {
	userID     uuid.UUID
	appearance string
	surface    string
}

type appearanceSpy struct{ calls []recorded }

func (s *appearanceSpy) AppearanceChanged(_ context.Context, userID uuid.UUID, appearance, surface string) {
	s.calls = append(s.calls, recorded{userID, appearance, surface})
}

func newAppearanceService(t *testing.T) (*users.Service, *appearanceSpy, users.User) {
	t.Helper()
	pool := testdb.New(t)
	spy := &appearanceSpy{}
	svc := users.NewService(users.NewRepository(pool)).WithAppearanceRecorder(spy)
	user, err := svc.Register(context.Background(), users.Registration{
		Email: "appearance@north.test", DisplayName: "Appearance", Timezone: "UTC",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	return svc, spy, user
}

func profileWith(a *theme.Appearance) users.Profile {
	return users.Profile{DisplayName: "Appearance", Timezone: "UTC", Appearance: a, Surface: "web_settings"}
}

func ptr(a theme.Appearance) *theme.Appearance { return &a }

// A new account has never chosen, and must read that way rather than as the
// web's default: iOS applies its own default to "never chosen".
func TestANewAccountHasNoAppearance(t *testing.T) {
	_, _, user := newAppearanceService(t)
	if user.Appearance != "" {
		t.Fatalf("appearance = %q, want empty (never chosen)", user.Appearance)
	}
}

func TestUpdateProfileStoresAnAppearanceAndReportsTheChange(t *testing.T) {
	svc, spy, user := newAppearanceService(t)
	ctx := context.Background()

	updated, err := svc.UpdateProfile(ctx, user.ID, profileWith(ptr(theme.Light)))
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Appearance != theme.Light {
		t.Fatalf("appearance = %q, want light", updated.Appearance)
	}
	if len(spy.calls) != 1 || spy.calls[0] != (recorded{user.ID, "light", "web_settings"}) {
		t.Fatalf("recorded %+v, want one light from web_settings", spy.calls)
	}
}

// Shipped iOS builds send the whole profile without the field. That must not
// wipe what the person chose, and must not count as a choice.
func TestUpdateProfileWithoutAnAppearanceLeavesTheStoredOne(t *testing.T) {
	svc, spy, user := newAppearanceService(t)
	ctx := context.Background()

	if _, err := svc.UpdateProfile(ctx, user.ID, profileWith(ptr(theme.Dark))); err != nil {
		t.Fatalf("set: %v", err)
	}
	updated, err := svc.UpdateProfile(ctx, user.ID, users.Profile{DisplayName: "Renamed", Timezone: "UTC"})
	if err != nil {
		t.Fatalf("update without appearance: %v", err)
	}
	if updated.Appearance != theme.Dark {
		t.Fatalf("appearance = %q, want dark kept", updated.Appearance)
	}
	if updated.DisplayName != "Renamed" {
		t.Fatalf("the rest of the profile was not saved: %+v", updated)
	}
	if len(spy.calls) != 1 {
		t.Fatalf("recorded %d changes, want only the first", len(spy.calls))
	}
}

// Saving the profile again with the same appearance is not a choice.
func TestUpdateProfileWithTheSameAppearanceRecordsNothing(t *testing.T) {
	svc, spy, user := newAppearanceService(t)
	ctx := context.Background()

	for range 2 {
		if _, err := svc.UpdateProfile(ctx, user.ID, profileWith(ptr(theme.System))); err != nil {
			t.Fatalf("update: %v", err)
		}
	}
	if len(spy.calls) != 1 {
		t.Fatalf("recorded %d changes, want 1", len(spy.calls))
	}
}

func TestUpdateProfileRefusesAnUnknownAppearance(t *testing.T) {
	svc, spy, user := newAppearanceService(t)

	_, err := svc.UpdateProfile(context.Background(), user.ID, profileWith(ptr("sepia")))
	var fields apperr.FieldErrors
	if !apperr.As(err, &fields) || fields.Messages()["appearance"] == "" {
		t.Fatalf("err = %v, want a field error on appearance", err)
	}
	if len(spy.calls) != 0 {
		t.Fatalf("recorded %+v for a refused value", spy.calls)
	}
}

func TestSetAppearanceReportsOnlyARealChange(t *testing.T) {
	svc, spy, user := newAppearanceService(t)
	ctx := context.Background()

	changed, err := svc.SetAppearance(ctx, user.ID, "light", "web_switcher")
	if err != nil || !changed {
		t.Fatalf("first set: changed=%v err=%v, want a change", changed, err)
	}
	changed, err = svc.SetAppearance(ctx, user.ID, "light", "web_switcher")
	if err != nil || changed {
		t.Fatalf("second set: changed=%v err=%v, want no change", changed, err)
	}
	if len(spy.calls) != 1 || spy.calls[0].surface != "web_switcher" {
		t.Fatalf("recorded %+v, want one from web_switcher", spy.calls)
	}

	got, err := svc.ByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.Appearance != theme.Light {
		t.Fatalf("stored %q, want light", got.Appearance)
	}
}

func TestSetAppearanceRefusesAnUnknownValue(t *testing.T) {
	svc, spy, user := newAppearanceService(t)

	if _, err := svc.SetAppearance(context.Background(), user.ID, "", "web_switcher"); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("err = %v, want a validation error", err)
	}
	if len(spy.calls) != 0 {
		t.Fatalf("recorded %+v for a refused value", spy.calls)
	}
}
