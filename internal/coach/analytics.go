package coach

import (
	"context"
	"log/slog"
	"time"

	"github.com/posthog/posthog-go"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/shared/metrics"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// Analytics reports the coach's LLM calls to PostHog's AI Observability, as a
// session -> trace -> generation/span tree rather than isolated events.
//
// A nil client (no PostHog project token configured in production) makes
// every method a no-op, so a deployment with no key coaches exactly as it
// did before this existed.
type Analytics struct {
	client  posthog.Client
	metrics *metrics.Registry
}

// NewAnalytics wraps a PostHog client. client may be nil.
func NewAnalytics(client posthog.Client) *Analytics {
	return &Analytics{client: client}
}

// WithMetrics attaches counters. Returns the analytics so it can be wired in
// one expression at startup; nil leaves counting off.
func (a *Analytics) WithMetrics(m *metrics.Registry) *Analytics {
	if a == nil {
		return a
	}
	a.metrics = m
	return a
}

// generation is one call to a model, whichever provider answered it.
type generation struct {
	sessionID  string
	traceID    string
	distinctID string
	provider   string
	model      string
	usage      ai.Usage
	latency    time.Duration
	err        error
}

// captureGeneration records one $ai_generation event.
//
// sessionID groups every turn of one conversation; traceID groups every
// generation of one turn. Both are required for the events to form a tree
// instead of landing as isolated rows — see the AI Observability skill this
// implements.
func (a *Analytics) captureGeneration(ctx context.Context, g generation) {
	// Logged before the PostHog check, and deliberately outside it. PostHog is
	// optional in production, so a deployment without it had no record of how
	// slow the coach was or what it spent — anywhere. trace_id is what joins
	// this line to the PostHog trace when both exist.
	logGeneration(ctx, g)

	// Counted here rather than at the call site, because there is more than one
	// call site: the turn itself, and the smaller call that names a
	// conversation. The second was logged but not counted when the counter
	// lived beside the first, which is exactly the kind of gap a second call
	// site opens.
	if a != nil {
		a.metrics.CoachGeneration(g.provider, g.latency, g.err != nil)
		a.metrics.CoachTokens(g.provider, g.usage.InputTokens, g.usage.OutputTokens)
	}

	if a == nil || a.client == nil {
		return
	}

	props := posthog.NewProperties().
		Set("$ai_trace_id", g.traceID).
		Set("$ai_session_id", g.sessionID).
		Set("$ai_provider", g.provider).
		Set("$ai_input_tokens", g.usage.InputTokens).
		Set("$ai_output_tokens", g.usage.OutputTokens).
		Set("$ai_latency", g.latency.Seconds())
	if g.model != "" {
		props.Set("$ai_model", g.model)
	}
	if g.err != nil {
		props.Set("$ai_is_error", true).Set("$ai_error", g.err.Error())
	}

	if err := a.client.Enqueue(posthog.Capture{
		DistinctId: g.distinctID,
		Event:      "$ai_generation",
		Properties: props,
	}); err != nil {
		middleware.FromContext(ctx).Warn("could not record ai generation", slog.Any("error", err))
	}
}

// captureToolSpan records one tool run as an $ai_span, a child of the turn's
// trace. The wrapper never sees the tool dispatch loop, so this is the only
// thing that records a tool run at all.
func (a *Analytics) captureToolSpan(ctx context.Context, sessionID, traceID, distinctID string, result ai.ToolResult) {
	if a == nil || a.client == nil {
		return
	}

	props := posthog.NewProperties().
		Set("$ai_trace_id", traceID).
		Set("$ai_session_id", sessionID).
		Set("$ai_span_name", result.Name).
		Set("$ai_parent_id", traceID).
		Set("$ai_is_error", result.IsError)

	if err := a.client.Enqueue(posthog.Capture{
		DistinctId: distinctID,
		Event:      "$ai_span",
		Properties: props,
	}); err != nil {
		middleware.FromContext(ctx).Warn("could not record tool span", slog.Any("error", err))
	}
}

// usageOrZero reads a possibly-nil usage pointer, such as the last chunk of a
// stream that ended before the provider reported one.
func usageOrZero(u *ai.Usage) ai.Usage {
	if u == nil {
		return ai.Usage{}
	}
	return *u
}

// logGeneration records one model call as a structured log line.
//
// Every field here is a number, an identifier, or a provider name. The prompt
// and the reply are deliberately absent: a person's conversation with their
// coach is the most sensitive thing North stores, and an observability change
// is exactly where it would leak by accident. See the test that pins it.
func logGeneration(ctx context.Context, g generation) {
	log := middleware.FromContext(ctx).With(
		slog.String("trace_id", g.traceID),
		slog.String("conversation_id", g.sessionID),
		slog.String("provider", g.provider),
		slog.String("model", g.model),
		slog.Int("input_tokens", g.usage.InputTokens),
		slog.Int("output_tokens", g.usage.OutputTokens),
		slog.Int64("latency_ms", g.latency.Milliseconds()),
	)

	if g.err != nil {
		log.Error("ai generation failed", slog.Any("error", g.err))
		return
	}
	log.Info("ai generation")
}
