package crews_test

import (
	"context"
	"errors"
	"testing"

	"github.com/NorthAIProject/north-client/internal/crews"
	"github.com/NorthAIProject/north-client/internal/crews/crew"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// fieldError is the message a validation failure carries for field, or "".
func fieldError(err error, field string) string {
	var fe apperr.FieldErrors
	if !errors.As(err, &fe) {
		return ""
	}
	return fe.Messages()[field]
}

// A new crew is a space with #general to talk in and #wins for what members
// share.
func TestCreateMakesGeneralAndWins(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := service(pool)
	ana := person(t, pool, "ana@north.test", "Ana")

	c, err := svc.Create(ctx, ana.ID, "Iron rats")
	if err != nil {
		t.Fatal(err)
	}
	channels, err := svc.Channels(ctx, c.ID, ana.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 2 ||
		channels[0].Name != "general" || channels[0].Kind != crew.ChannelText || !channels[0].IsDefault ||
		channels[1].Name != "wins" || channels[1].Kind != crew.ChannelWins || channels[1].IsDefault {
		t.Fatalf("channels = %+v", channels)
	}
}

func TestChannelsMembersOnly(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := service(pool)
	ana := person(t, pool, "ana@north.test", "Ana")
	sam := person(t, pool, "sam@north.test", "Sam")
	c, _ := svc.Create(ctx, ana.ID, "Iron rats")

	if _, err := svc.Channels(ctx, c.ID, sam.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("outsider listed channels: %v", err)
	}
}

// The owner names admins; admins run the challenge and remove plain members,
// but never the owner, another admin, or anyone's role.
func TestRoles(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := service(pool)
	ana := person(t, pool, "ana@north.test", "Ana")
	leo := person(t, pool, "leo@north.test", "Leo")
	mia := person(t, pool, "mia@north.test", "Mia")
	zed := person(t, pool, "zed@north.test", "Zed")
	c, _ := svc.Create(ctx, ana.ID, "Iron rats")
	for _, id := range []users.User{leo, mia, zed} {
		if _, err := svc.Join(ctx, id.ID, c.Code); err != nil {
			t.Fatal(err)
		}
	}

	if err := svc.SetRole(ctx, c.ID, ana.ID, leo.ID, crew.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	board, err := svc.Board(ctx, c.ID, leo.ID)
	if err != nil || board.MyRole != crew.RoleAdmin {
		t.Fatalf("leo's role = %q, %v", board.MyRole, err)
	}
	for _, m := range board.Members {
		want := map[string]crew.Role{"Ana": crew.RoleOwner, "Leo": crew.RoleAdmin, "Mia": crew.RoleMember, "Zed": crew.RoleMember}[m.DisplayName]
		if m.Role != want {
			t.Errorf("%s is %q; want %q", m.DisplayName, m.Role, want)
		}
	}

	if err = svc.SetRole(ctx, c.ID, leo.ID, mia.ID, crew.RoleAdmin); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("an admin set a role: %v", err)
	}
	if err = svc.SetRole(ctx, c.ID, ana.ID, ana.ID, crew.RoleMember); fieldError(err, "role") == "" {
		t.Fatalf("the owner changed their own role: %v", err)
	}
	if err = svc.SetRole(ctx, c.ID, ana.ID, mia.ID, crew.RoleOwner); fieldError(err, "role") == "" {
		t.Fatalf("ownership given through a role: %v", err)
	}

	if err = svc.SetChallenge(ctx, c.ID, leo.ID, &crews.Challenge{Kind: crew.ChallengeWorkouts, Target: 3}); err != nil {
		t.Fatalf("admin could not set the challenge: %v", err)
	}
	if err = svc.Remove(ctx, c.ID, leo.ID, mia.ID); err != nil {
		t.Fatalf("admin could not remove a member: %v", err)
	}
	if err = svc.Remove(ctx, c.ID, leo.ID, ana.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("admin removed the owner: %v", err)
	}
	if err = svc.SetRole(ctx, c.ID, ana.ID, zed.ID, crew.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err = svc.Remove(ctx, c.ID, leo.ID, zed.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("admin removed another admin: %v", err)
	}
}

// An owner who leaves hands the crew to the longest-standing admin before
// the longest-standing member.
func TestOwnerLeavingPrefersAdmin(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := service(pool)
	ana := person(t, pool, "ana@north.test", "Ana")
	leo := person(t, pool, "leo@north.test", "Leo")
	mia := person(t, pool, "mia@north.test", "Mia")
	c, _ := svc.Create(ctx, ana.ID, "Iron rats")
	if _, err := svc.Join(ctx, leo.ID, c.Code); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Join(ctx, mia.ID, c.Code); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetRole(ctx, c.ID, ana.ID, mia.ID, crew.RoleAdmin); err != nil {
		t.Fatal(err)
	}

	if err := svc.Leave(ctx, c.ID, ana.ID); err != nil {
		t.Fatal(err)
	}
	board, err := svc.Board(ctx, c.ID, leo.ID)
	if err != nil || board.OwnerID != mia.ID {
		t.Fatalf("owner after ana left = %v (%v); want mia", board.OwnerID, err)
	}
}
