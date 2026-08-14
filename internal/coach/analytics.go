package coach

import (
	"context"
	"log/slog"
	"time"

	"github.com/posthog/posthog-go"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// Analytics reports the coach's LLM calls to PostHog's AI Observability, as a
// session -> trace -> generation/span tree rather than isolated events.
//
// A nil client (no PostHog project token configured in production) makes
// every method a no-op, so a deployment with no key coaches exactly as it
// did before this existed.
type Analytics struct {
	client posthog.Client
}

// NewAnalytics wraps a PostHog client. client may be nil.
func NewAnalytics(client posthog.Client) *Analytics {
	return &Analytics{client: client}
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
