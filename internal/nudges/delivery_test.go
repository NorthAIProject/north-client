package nudges_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/nudges"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// pushSpy stands in for internal/push. It records what it was asked to send and
// answers with a fixed delivered count.
type pushSpy struct {
	delivered int
	sent      []pushCall
}

type pushCall struct {
	userID            uuid.UUID
	title, body, href string
}

func (p *pushSpy) Send(_ context.Context, userID uuid.UUID, title, body, href string) (int, error) {
	p.sent = append(p.sent, pushCall{userID: userID, title: title, body: body, href: href})
	return p.delivered, nil
}

type funnelSpy struct {
	delivered []string // kind/channel
	opened    []string
}

func (f *funnelSpy) NudgeDelivered(_ context.Context, _ uuid.UUID, kind, channel string) {
	f.delivered = append(f.delivered, kind+"/"+channel)
}

func (f *funnelSpy) NudgeOpened(_ context.Context, _ uuid.UUID, kind, channel string) {
	f.opened = append(f.opened, kind+"/"+channel)
}

// noon is safely outside the default quiet hours in UTC, which is the zone
// seedUser gives every account.
var noon = time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

func TestRaiseSendsToBrowsersThroughTheOpenLink(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "nudge-push@north.test")
	pushes := &pushSpy{delivered: 1}
	funnel := &funnelSpy{}
	svc := newStore(pool).WithClock(freeze(noon)).WithPush(pushes).WithFunnel(funnel)

	n, created, err := svc.Raise(ctx, user, nudges.Draft{
		Kind:      nudges.KindMissedCheckIn,
		DedupeKey: "2026-09-02",
		Title:     "Check in with yourself",
		Body:      "It has been 3 days since your last check-in.",
		Href:      "/app/check-ins",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("first raise should create")
	}

	if len(pushes.sent) != 1 {
		t.Fatalf("push sent %d times, want 1", len(pushes.sent))
	}
	got := pushes.sent[0]
	if got.userID != user.ID || got.title != "Check in with yourself" || !strings.Contains(got.body, "3 days") {
		t.Fatalf("push carried %+v", got)
	}
	// Not the nudge's own href: the tap has to pass through /open so the
	// server can count it before redirecting to /app/check-ins.
	want := "/app/nudges/" + n.ID.String() + "/open?from=push"
	if got.href != want {
		t.Fatalf("push href = %q, want %q", got.href, want)
	}

	// Delivered once to the bell (the insert) and once to push (accepted).
	if strings.Join(funnel.delivered, ",") != "missed_checkin/bell,missed_checkin/push" {
		t.Fatalf("funnel saw %v", funnel.delivered)
	}

	// The same draft again is a no-op everywhere: no second row, no second push.
	if _, created, err := svc.Raise(ctx, user, nudges.Draft{
		Kind: nudges.KindMissedCheckIn, DedupeKey: "2026-09-02", Title: "again",
	}); err != nil || created {
		t.Fatalf("duplicate raise: created=%v err=%v", created, err)
	}
	if len(pushes.sent) != 1 {
		t.Fatalf("duplicate raise pushed again: %d sends", len(pushes.sent))
	}
}

// A push the service tried but nobody accepted — no subscriptions, or the
// push service refused — must not be counted as delivered.
func TestRaiseDoesNotCountAPushNobodyReceived(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "nudge-push-none@north.test")
	funnel := &funnelSpy{}
	svc := newStore(pool).WithClock(freeze(noon)).WithPush(&pushSpy{delivered: 0}).WithFunnel(funnel)

	if _, _, err := svc.Raise(ctx, user, nudges.Draft{
		Kind: nudges.KindGoalDeadline, DedupeKey: "g:1", Title: "Due", Href: "/app/goals/1",
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(funnel.delivered, ",") != "goal_deadline/bell" {
		t.Fatalf("funnel saw %v, want the bell only", funnel.delivered)
	}
}

// Coach replies and briefings already reached the person another way; they
// stay in the bell and are never pushed, exactly as they were never sent to
// Telegram.
func TestRaiseKeepsInAppOnlyKindsOutOfPush(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "nudge-push-quiet@north.test")
	pushes := &pushSpy{delivered: 1}
	funnel := &funnelSpy{}
	svc := newStore(pool).WithClock(freeze(noon)).WithPush(pushes).WithFunnel(funnel)

	for _, kind := range []string{nudges.KindCoachReply, nudges.KindBriefingReady} {
		if _, _, err := svc.Raise(ctx, user, nudges.Draft{Kind: kind, DedupeKey: kind, Title: "t"}); err != nil {
			t.Fatal(err)
		}
	}
	if len(pushes.sent) != 0 {
		t.Fatalf("in-app kinds were pushed: %+v", pushes.sent)
	}
	if strings.Join(funnel.delivered, ",") != "coach_reply/bell,briefing_ready/bell" {
		t.Fatalf("funnel saw %v", funnel.delivered)
	}
}

func TestOpenMarksReadAndAttributesTheChannel(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "nudge-open@north.test")
	funnel := &funnelSpy{}
	svc := newStore(pool).WithClock(freeze(noon)).WithFunnel(funnel)

	n, _, err := svc.Raise(ctx, user, nudges.Draft{
		Kind: nudges.KindWorkoutToday, DedupeKey: "2026-09-02", Title: "Leg day", Href: "/app/workouts",
	})
	if err != nil {
		t.Fatal(err)
	}

	opened, err := svc.Open(ctx, n.ID, user.ID, "push")
	if err != nil {
		t.Fatal(err)
	}
	if opened.ReadAt == nil {
		t.Fatal("open did not mark the nudge read")
	}
	if opened.Href != "/app/workouts" {
		t.Fatalf("open returned href %q", opened.Href)
	}
	if strings.Join(funnel.opened, ",") != "workout_today/push" {
		t.Fatalf("funnel saw %v", funnel.opened)
	}

	// Somebody else's nudge, or a dismissed one, is not found.
	other := seedUser(t, pool, "nudge-open-other@north.test")
	if _, err := svc.Open(ctx, n.ID, other.ID, "push"); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("open across accounts: %v, want not found", err)
	}
	if _, err := svc.Dismiss(ctx, n.ID, user.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Open(ctx, n.ID, user.ID, "bell"); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("open after dismiss: %v, want not found", err)
	}
	if len(funnel.opened) != 1 {
		t.Fatalf("refused opens were counted: %v", funnel.opened)
	}
}

func TestOpenPathCarriesTheChannel(t *testing.T) {
	id := uuid.MustParse("11111111-2222-3333-4444-555555555555")
	got := nudges.OpenPath(id, "push")
	want := "/app/nudges/11111111-2222-3333-4444-555555555555/open?from=push"
	if got != want {
		t.Fatalf("OpenPath = %q, want %q", got, want)
	}
}
