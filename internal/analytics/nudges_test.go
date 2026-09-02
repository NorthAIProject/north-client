package analytics_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/analytics"
)

// The nudge events are the ones the native-app decision is read from, so the
// names and the two properties they break down by are the contract here, in
// the same way the funnel test pins the activation events.
func TestTheNudgeEventsCarryKindAndChannel(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	f := analytics.New(rec)
	user := uuid.New()
	ctx := context.Background()

	f.PushSubscribed(ctx, user)
	f.NudgeDelivered(ctx, user, "missed_checkin", analytics.ChannelPush)
	f.NudgeOpened(ctx, user, "missed_checkin", analytics.ChannelBell)
	f.MomentShown(ctx, user, "streak")

	want := []string{"push_subscribed", "nudge_delivered", "nudge_opened", "moment_shown"}
	if len(rec.captures) != len(want) {
		t.Fatalf("captured %d events, want %d", len(rec.captures), len(want))
	}
	for i, name := range want {
		if rec.captures[i].Event != name {
			t.Errorf("event %d is %q, want %q", i, rec.captures[i].Event, name)
		}
		if rec.captures[i].DistinctId != user.String() {
			t.Errorf("event %q attributed to %q, want the user", name, rec.captures[i].DistinctId)
		}
	}

	if got := rec.captures[1].Properties["channel"]; got != "push" {
		t.Errorf("nudge_delivered channel = %v, want push", got)
	}
	if got := rec.captures[1].Properties["kind"]; got != "missed_checkin" {
		t.Errorf("nudge_delivered kind = %v, want missed_checkin", got)
	}
	if got := rec.captures[2].Properties["channel"]; got != "bell" {
		t.Errorf("nudge_opened channel = %v, want bell", got)
	}
	if got := rec.captures[3].Properties["kind"]; got != "streak" {
		t.Errorf("moment_shown kind = %v, want streak", got)
	}
}

func TestTheNudgeEventsAreSilentWithoutAClient(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	user := uuid.New()
	for name, f := range map[string]*analytics.Funnel{
		"nil funnel": nil,
		"nil client": analytics.New(nil),
	} {
		t.Run(name, func(t *testing.T) {
			f.PushSubscribed(ctx, user)
			f.NudgeDelivered(ctx, user, "k", analytics.ChannelBell)
			f.NudgeOpened(ctx, user, "k", analytics.ChannelPush)
			f.MomentShown(ctx, user, "k")
		})
	}
}
