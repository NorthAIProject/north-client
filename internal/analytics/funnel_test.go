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

	f.Registered(ctx, user, analytics.ViaPassword)
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

	// Literal again, for the same reason as the event names: the door an
	// account came through is what says whether the connector is acquiring
	// anybody, and a rename that silently changed it would read as zero.
	if got := rec.captures[0].Properties["via"]; got != "password" {
		t.Errorf("user_registered carried via=%v, want password", got)
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
			f.Registered(ctx, user, analytics.ViaPassword)
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
	analytics.New(rec).Registered(context.Background(), uuid.New(), analytics.ViaPassword)
}

// An event with no user cannot be joined to anything and would pollute the
// funnel with an anonymous row.
func TestAnEventWithoutAUserIsDropped(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	analytics.New(rec).Registered(context.Background(), uuid.Nil, analytics.ViaPassword)

	if len(rec.captures) != 0 {
		t.Fatalf("captured %d events for a nil user, want none", len(rec.captures))
	}
}

// The two most important events in the connector funnel happen before there is
// an account: a client registering, and somebody reaching the consent screen.
// capture drops those on purpose — a nil user cannot be joined to anything —
// so captureAnon is what keeps the denominator of every conversion rate from
// being empty.
func TestTheAnonymousEventsAreRecordedWithoutAnAccount(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	f := analytics.New(rec)
	ctx := context.Background()

	f.MCPClientRegistered(ctx, "mcpc_abc", "Claude Code", "claude-code")
	f.MCPAuthorizeStarted(ctx, "11111111-2222-3333-4444-555555555555", "Claude Code", "north:read_write", false)

	if len(rec.captures) != 2 {
		t.Fatalf("captured %d anonymous events, want 2", len(rec.captures))
	}

	// Literals, for the same reason the other event names are literals here:
	// the PostHog insights are defined against these exact strings.
	if rec.captures[0].Event != "mcp_client_registered" {
		t.Errorf("first event is %q", rec.captures[0].Event)
	}
	if rec.captures[1].Event != "mcp_authorize_started" {
		t.Errorf("second event is %q", rec.captures[1].Event)
	}

	// The distinct id is the thing a later identify call stitches to an
	// account. A registration is keyed on the client; a consent visit on the
	// authorization request it belongs to.
	if rec.captures[0].DistinctId != "mcpc_abc" {
		t.Errorf("a registration is attributed to %q, want the client id", rec.captures[0].DistinctId)
	}
	if rec.captures[1].DistinctId != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("a consent visit is attributed to %q, want the request id", rec.captures[1].DistinctId)
	}
	if rec.captures[1].Properties["signed_in"] != false {
		t.Errorf("signed_in is %v, want false", rec.captures[1].Properties["signed_in"])
	}
}

// An anonymous event with no distinct id at all cannot be joined to anything
// either, so it is dropped the way a nil user is.
func TestAnAnonymousEventWithNoIdentityIsDropped(t *testing.T) {
	t.Parallel()

	rec := &recorder{}
	analytics.New(rec).MCPClientRegistered(context.Background(), "", "Claude Code", "")

	if len(rec.captures) != 0 {
		t.Fatalf("captured %d events with no distinct id, want none", len(rec.captures))
	}
}

// A nil funnel and a nil client stay silent for the anonymous path too.
func TestTheAnonymousPathIsAlsoSilentWithoutAClient(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for name, f := range map[string]*analytics.Funnel{
		"nil funnel": nil,
		"nil client": analytics.New(nil),
	} {
		t.Run(name, func(t *testing.T) {
			f.MCPClientRegistered(ctx, "mcpc_abc", "Claude Code", "")
			f.MCPAuthorizeStarted(ctx, "req", "Claude Code", "", true)
		})
	}
}
