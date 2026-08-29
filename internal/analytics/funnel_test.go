package analytics_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/posthog/posthog-go"

	"github.com/NorthAIProject/north-client/internal/analytics"
)

// recorder stands in for the PostHog client. What reaches the wire is the only
// thing worth asserting: the event names are the strings the dashboard insights
// are defined against, and a typo here is invisible until a funnel silently
// reads zero.
type recorder struct {
	// Embedded rather than implemented. posthog.Client is a wide, growing
	// interface and this test cares about exactly one method; spelling out the
	// feature-flag half would be noise that breaks on every SDK bump. The
	// embedded value is nil, so anything other than Enqueue panics loudly
	// rather than passing quietly — which is the behaviour we want if the
	// funnel ever starts calling something else.
	posthog.Client

	captures []posthog.Capture
	err      error
}

func (r *recorder) Enqueue(msg posthog.Message) error {
	if c, ok := msg.(posthog.Capture); ok {
		r.captures = append(r.captures, c)
	}
	return r.err
}

func TestTheFunnelEmitsTheEventNamesTheInsightsAreBuiltOn(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	f := analytics.New(rec)
	user := uuid.New()
	ctx := context.Background()

	f.Registered(ctx, user)
	f.OnboardingCompleted(ctx, user)
	f.SourceConnected(ctx, user, analytics.SourceStrava)
	f.CoachReplied(ctx, user, "telegram")

	// Literals, not the constants. Comparing a constant to itself would pass
	// through any rename, and the string on the wire is the contract: the
	// PostHog insights are defined against these exact names, and a typo is
	// invisible until a funnel quietly reads zero forever.
	want := []string{
		"user_registered",
		"onboarding_completed",
		"source_connected",
		"coach_replied",
	}
	if len(rec.captures) != len(want) {
		t.Fatalf("captured %d events, want %d", len(rec.captures), len(want))
	}
	for i, name := range want {
		if rec.captures[i].Event != name {
			t.Errorf("event %d is %q, want %q", i, rec.captures[i].Event, name)
		}
		if rec.captures[i].DistinctId != user.String() {
			t.Errorf("event %q is attributed to %q, want the user id",
				name, rec.captures[i].DistinctId)
		}
	}

	// The properties the funnel actually breaks down by.
	if got := rec.captures[2].Properties["source"]; got != "strava" {
		t.Errorf("source_connected carried source=%v, want strava", got)
	}
	if got := rec.captures[3].Properties["surface"]; got != "telegram" {
		t.Errorf("coach_replied carried surface=%v, want telegram", got)
	}
}

// A deployment with no PostHog key must behave exactly as it did before this
// package existed. Every call site is a single unguarded line, so this is the
// thing standing between "no analytics configured" and a nil dereference on
// signup.
func TestAFunnelWithoutAClientIsSilentRatherThanFatal(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	user := uuid.New()

	for name, f := range map[string]*analytics.Funnel{
		"nil funnel": nil,
		"nil client": analytics.New(nil),
	} {
		t.Run(name, func(t *testing.T) {
			f.Registered(ctx, user)
			f.OnboardingCompleted(ctx, user)
			f.SourceConnected(ctx, user, analytics.SourceTelegram)
			f.CoachReplied(ctx, user, "web")
		})
	}
}

// Analytics must never be able to fail a request that already succeeded.
func TestAFailingClientDoesNotPanicOrPropagate(t *testing.T) {
	t.Parallel()

	rec := &recorder{err: errors.New("posthog is down")}
	analytics.New(rec).Registered(context.Background(), uuid.New())
}

// An event with no user cannot be joined to anything and would pollute the
// funnel with an anonymous row.
func TestAnEventWithoutAUserIsDropped(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	analytics.New(rec).Registered(context.Background(), uuid.Nil)

	if len(rec.captures) != 0 {
		t.Fatalf("captured %d events for a nil user, want none", len(rec.captures))
	}
}
