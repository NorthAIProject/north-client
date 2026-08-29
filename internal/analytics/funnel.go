// Package analytics reports the product funnel: the handful of events that say
// whether a stranger got anywhere.
//
// Separate from coach.Analytics, which reports LLM calls to PostHog's AI
// Observability. That answers "what did the model cost and how long did it
// take". This answers "did anybody activate", and until now nothing did — the
// only events the application emitted were $ai_generation and $ai_span, so a
// launch would have produced excellent traces of an empty room.
//
// Five events, deliberately. A funnel with thirty steps is a funnel nobody
// reads, and every event that is not on the path from stranger to activation is
// a decision deferred rather than made.
package analytics

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/posthog/posthog-go"

	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// Event names. Exported so the PostHog insight definitions and the tests can
// name the same strings the code emits.
const (
	// EventRegistered is an account created. The top of the funnel.
	EventRegistered = "user_registered"

	// EventOnboardingCompleted is onboarding finished rather than skipped.
	EventOnboardingCompleted = "onboarding_completed"

	// EventSourceConnected is the first real signal. Nothing about North works
	// before a source exists — the coach has nothing of yours to reason over —
	// so this is the step people drop at and the one worth watching.
	EventSourceConnected = "source_connected"

	// EventCoachReplied is a completed coach answer, on whichever surface.
	// Activation is two of these on different days, which is derived in
	// PostHog rather than computed here: the second day is a property of the
	// history, not of the reply.
	EventCoachReplied = "coach_replied"
)

// Sources for EventSourceConnected.
const (
	SourceStrava   = "strava"
	SourceTelegram = "telegram"
	SourceDocument = "document"
	SourceCalendar = "calendar"
)

// Funnel reports the product funnel.
//
// A nil *Funnel, and a Funnel holding a nil client, are both no-ops: a
// deployment with no PostHog key behaves exactly as it did before this
// existed. That is the same arrangement coach.Analytics uses, and it is what
// lets every call site below stay a single unguarded line.
type Funnel struct {
	client posthog.Client
}

// New wraps a PostHog client. client may be nil.
func New(client posthog.Client) *Funnel {
	return &Funnel{client: client}
}

// Registered records a new account.
func (p *Funnel) Registered(ctx context.Context, userID uuid.UUID) {
	p.capture(ctx, userID, EventRegistered, nil)
}

// OnboardingCompleted records onboarding finished. Skipping is deliberately
// not this event: the question the funnel asks is how many people answered,
// and counting a skip as a completion would answer it wrongly in the
// flattering direction.
func (p *Funnel) OnboardingCompleted(ctx context.Context, userID uuid.UUID) {
	p.capture(ctx, userID, EventOnboardingCompleted, nil)
}

// SourceConnected records a data source arriving: Strava, Telegram, a
// document, a calendar.
func (p *Funnel) SourceConnected(ctx context.Context, userID uuid.UUID, source string) {
	p.capture(ctx, userID, EventSourceConnected, posthog.Properties{"source": source})
}

// CoachReplied records a completed answer. surface distinguishes the web chat
// from Telegram and MCP, because "they came back the next day" means something
// different when the second day happened in a messaging app.
func (p *Funnel) CoachReplied(ctx context.Context, userID uuid.UUID, surface string) {
	p.capture(ctx, userID, EventCoachReplied, posthog.Properties{"surface": surface})
}

// capture is the one place an event reaches PostHog.
//
// Analytics must never be able to fail a request that succeeded, so an enqueue
// error is logged and swallowed. The log line is what makes a silent PostHog
// diagnosable — the last time a project shipped without it, months of "we have
// no users" turned out to be events going to a project nobody could see.
func (p *Funnel) capture(ctx context.Context, userID uuid.UUID, event string, props posthog.Properties) {
	if p == nil || p.client == nil {
		return
	}
	if userID == uuid.Nil {
		return
	}

	if props == nil {
		props = posthog.Properties{}
	}
	if requestID := middleware.RequestIDFrom(ctx); requestID != "" {
		props.Set("request_id", requestID)
	}

	if err := p.client.Enqueue(posthog.Capture{
		DistinctId: userID.String(),
		Event:      event,
		Properties: props,
	}); err != nil {
		middleware.FromContext(ctx).Warn("could not record a product event",
			slog.String("event", event), slog.Any("error", err))
	}
}
