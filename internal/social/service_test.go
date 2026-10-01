package social_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/social"
	"github.com/NorthAIProject/north-client/internal/users"
)

type note struct {
	to          uuid.UUID
	kind, title string
}

type inboxSpy struct {
	mu    sync.Mutex
	notes []note
}

func (s *inboxSpy) NoteWithPush(_ context.Context, userID uuid.UUID, kind, _, title, _, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notes = append(s.notes, note{userID, kind, title})
	return nil
}

type funnelSpy struct{ channels []string }

func (f *funnelSpy) InviteRedeemed(_ context.Context, _ uuid.UUID, channel string) {
	f.channels = append(f.channels, channel)
}

func person(t *testing.T, pool *pgxpool.Pool, email, name string) users.User {
	t.Helper()
	u, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email: email, PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: name, Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func fixture(t *testing.T) (*social.Service, *inboxSpy, *funnelSpy, *pgxpool.Pool) {
	t.Helper()
	pool := testdb.New(t)
	inbox, funnel := &inboxSpy{}, &funnelSpy{}
	return social.NewService(social.NewRepository(pool)).WithInbox(inbox).WithFunnel(funnel), inbox, funnel, pool
}

func TestHandles(t *testing.T) {
	svc, _, _, pool := fixture(t)
	ctx := context.Background()
	ana := person(t, pool, "ana@north.test", "Ana")
	joao := person(t, pool, "joao@north.test", "João")

	got, err := svc.SetHandle(ctx, ana.ID, " @Ana_Runs ")
	if err != nil || got != "ana_runs" {
		t.Fatalf("handle = %q, err = %v; want ana_runs", got, err)
	}
	for raw, why := range map[string]string{"ab": "too short", "ana runs": "space", "support": "reserved", "ANA_RUNS": "taken, case-blind"} {
		if _, err := svc.SetHandle(ctx, joao.ID, raw); !apperr.Is(err, apperr.ErrValidation) {
			t.Fatalf("%q (%s) accepted: %v", raw, why, err)
		}
	}
	// Clearing makes the account impossible to look up.
	if _, err := svc.SetHandle(ctx, ana.ID, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Profile(ctx, joao.ID, "ana_runs"); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("cleared handle still found: %v", err)
	}
}

// An invite link connects both ways, tells the inviter, and counts the
// channel; redeeming again, or your own link, changes nothing.
func TestInviteRedeemConnectsBothWays(t *testing.T) {
	svc, inbox, funnel, pool := fixture(t)
	ctx := context.Background()
	ana := person(t, pool, "ana@north.test", "Ana")
	joao := person(t, pool, "joao@north.test", "João")

	invite, err := svc.InviteFor(ctx, ana.ID, social.ChannelX)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := svc.InviteFor(ctx, ana.ID, social.ChannelX)
	if again.Code != invite.Code || len(invite.Code) != 10 {
		t.Fatalf("codes %q then %q, want one stable 10-char code per channel", invite.Code, again.Code)
	}
	preview, err := svc.PreviewInvite(ctx, invite.Code)
	if err != nil || preview.Inviter.DisplayName != "Ana" {
		t.Fatalf("preview = %+v, %v", preview, err)
	}

	if ok, err := svc.Redeem(ctx, ana.ID, invite.Code); err != nil || ok {
		t.Fatalf("own invite redeemed: %v %v", ok, err)
	}
	if ok, err := svc.Redeem(ctx, joao.ID, invite.Code); err != nil || !ok {
		t.Fatalf("redeem = %v, %v", ok, err)
	}
	if ok, _ := svc.Redeem(ctx, joao.ID, invite.Code); ok {
		t.Fatal("second redeem reported as a new connection")
	}

	for _, pair := range [][2]users.User{{ana, joao}, {joao, ana}} {
		overview, err := svc.Overview(ctx, pair[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		if len(overview.Followers) != 1 || overview.Followers[0].ID != pair[1].ID ||
			len(overview.Following) != 1 || overview.Following[0].Status != social.StatusAccepted {
			t.Fatalf("%s overview = %+v", pair[0].DisplayName, overview)
		}
	}
	anaView, _ := svc.Overview(ctx, ana.ID)
	if anaView.Joined != 1 {
		t.Fatalf("joined = %d, want 1", anaView.Joined)
	}
	if len(inbox.notes) != 1 || inbox.notes[0].to != ana.ID || inbox.notes[0].kind != social.KindInviteJoined {
		t.Fatalf("notes = %+v", inbox.notes)
	}
	if len(funnel.channels) != 1 || funnel.channels[0] != social.ChannelX {
		t.Fatalf("funnel = %v", funnel.channels)
	}

	// A code nobody holds is dropped quietly, not a failed signup.
	if ok, err := svc.Redeem(ctx, joao.ID, "zzzzzzzzzz"); err != nil || ok {
		t.Fatalf("unknown code: %v %v", ok, err)
	}
}

// A follow waits for the other person's yes, and they hear about it once.
func TestFollowNeedsApproval(t *testing.T) {
	svc, inbox, _, pool := fixture(t)
	ctx := context.Background()
	ana := person(t, pool, "ana@north.test", "Ana")
	joao := person(t, pool, "joao@north.test", "João")
	if _, err := svc.SetHandle(ctx, joao.ID, "joao"); err != nil {
		t.Fatal(err)
	}

	c, err := svc.Follow(ctx, ana.ID, "@joao")
	if err != nil || c.Status != social.StatusPending {
		t.Fatalf("follow = %+v, %v", c, err)
	}
	if _, err = svc.Follow(ctx, ana.ID, "joao"); err != nil {
		t.Fatal(err)
	}
	if len(inbox.notes) != 1 || inbox.notes[0].kind != social.KindFollowRequest || inbox.notes[0].to != joao.ID {
		t.Fatalf("notes = %+v, want one request", inbox.notes)
	}

	joaoView, _ := svc.Overview(ctx, joao.ID)
	if len(joaoView.Requests) != 1 || len(joaoView.Followers) != 0 {
		t.Fatalf("before accepting: %+v", joaoView)
	}
	if err = svc.Accept(ctx, joao.ID, ana.ID); err != nil {
		t.Fatal(err)
	}
	profile, err := svc.Profile(ctx, ana.ID, "joao")
	if err != nil || profile.Relationship.Following != social.StatusAccepted || profile.Followers != 1 {
		t.Fatalf("profile = %+v, %v", profile, err)
	}

	// Removing a follower is silent and reversible by them asking again.
	if err := svc.RemoveFollower(ctx, joao.ID, ana.ID); err != nil {
		t.Fatal(err)
	}
	if p, _ := svc.Profile(ctx, ana.ID, "joao"); p.Relationship.Following != "" {
		t.Fatalf("still following after removal: %+v", p.Relationship)
	}
}

// A block ends every follow between the two and hides each from the other,
// including from their invite link.
func TestBlockHidesBothWays(t *testing.T) {
	svc, _, _, pool := fixture(t)
	ctx := context.Background()
	ana := person(t, pool, "ana@north.test", "Ana")
	joao := person(t, pool, "joao@north.test", "João")
	_, _ = svc.SetHandle(ctx, ana.ID, "ana")
	_, _ = svc.SetHandle(ctx, joao.ID, "joao")
	invite, _ := svc.InviteFor(ctx, ana.ID, social.ChannelLink)

	if _, err := svc.Follow(ctx, joao.ID, "ana"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Block(ctx, ana.ID, joao.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Profile(ctx, joao.ID, "ana"); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("blocked person sees the profile: %v", err)
	}
	if _, err := svc.Profile(ctx, ana.ID, "joao"); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("blocker sees the profile: %v", err)
	}
	if _, err := svc.Follow(ctx, joao.ID, "ana"); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("blocked person can ask to follow: %v", err)
	}
	if ok, _ := svc.Redeem(ctx, joao.ID, invite.Code); ok {
		t.Fatal("blocked person connected through the invite link")
	}
	anaView, _ := svc.Overview(ctx, ana.ID)
	if len(anaView.Requests)+len(anaView.Followers)+len(anaView.Following) != 0 || len(anaView.Blocked) != 1 {
		t.Fatalf("after block: %+v", anaView)
	}
}

// A friend who is already on Khepri is connected by your link too, but only
// an account's first invite counts as the one that brought them in.
func TestInviteConnectsAFriendAlreadyHere(t *testing.T) {
	svc, inbox, funnel, pool := fixture(t)
	ctx := context.Background()
	leo := person(t, pool, "leo@north.test", "Leo")
	zoe := person(t, pool, "zoe@north.test", "Zoe")
	mia := person(t, pool, "mia@north.test", "Mia")
	leoInvite, _ := svc.InviteFor(ctx, leo.ID, social.ChannelLink)
	zoeInvite, _ := svc.InviteFor(ctx, zoe.ID, social.ChannelLink)

	if ok, err := svc.Redeem(ctx, mia.ID, leoInvite.Code); err != nil || !ok {
		t.Fatalf("first invite: %v %v", ok, err)
	}
	if ok, err := svc.Redeem(ctx, mia.ID, zoeInvite.Code); err != nil || !ok {
		t.Fatalf("second friend's invite did not connect: %v %v", ok, err)
	}

	mine, _ := svc.Overview(ctx, mia.ID)
	if len(mine.Following) != 2 || len(mine.Followers) != 2 {
		t.Fatalf("mia = %+v, want both friends both ways", mine)
	}
	if o, _ := svc.Overview(ctx, zoe.ID); o.Joined != 0 {
		t.Fatalf("zoe joined = %d; mia was already here", o.Joined)
	}
	if o, _ := svc.Overview(ctx, leo.ID); o.Joined != 1 {
		t.Fatalf("leo joined = %d, want 1", o.Joined)
	}
	if len(funnel.channels) != 1 {
		t.Fatalf("funnel counted %d invites, want only the first", len(funnel.channels))
	}
	if len(inbox.notes) != 2 || inbox.notes[1].title != "Mia accepted your invite" {
		t.Fatalf("notes = %+v", inbox.notes)
	}
}

func emailHash(email string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(email)))
	return hex.EncodeToString(sum[:])
}

// Contacts find people who chose a handle, never yourself or anybody blocked
// either way, and say how you already follow them.
func TestMatchContacts(t *testing.T) {
	svc, _, _, pool := fixture(t)
	ctx := context.Background()
	me := person(t, pool, "me@north.test", "Me")
	ana := person(t, pool, "Ana@North.test", "Ana")
	hidden := person(t, pool, "hidden@north.test", "Hidden")
	blocked := person(t, pool, "blocked@north.test", "Blocked")
	_, _ = svc.SetHandle(ctx, ana.ID, "ana")
	_, _ = svc.SetHandle(ctx, blocked.ID, "blocked")
	_, _ = svc.SetHandle(ctx, me.ID, "me")
	_ = hidden
	if err := svc.Block(ctx, blocked.ID, me.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Follow(ctx, me.ID, "ana"); err != nil {
		t.Fatal(err)
	}

	hashes := []string{emailHash("ana@north.test"), emailHash("hidden@north.test"), emailHash("blocked@north.test"), emailHash("me@north.test"), emailHash("nobody@north.test")}
	found, err := svc.MatchContacts(ctx, me.ID, hashes)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].ID != ana.ID || found[0].Status != social.StatusPending {
		t.Fatalf("found = %+v, want only Ana, already asked", found)
	}

	if _, err := svc.MatchContacts(ctx, me.ID, []string{"ana@north.test"}); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("a raw email was accepted: %v", err)
	}
}
