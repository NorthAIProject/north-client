package coach_test

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// Latency and token counts went only to PostHog, which is optional in
// production — so a deployment without it had no record of how slow the coach
// was or what it spent, anywhere.
func TestAGenerationIsLoggedWithItsCostAndLatency(t *testing.T) {
	var buf bytes.Buffer
	h := newHarness(t, fake.Text("Add two and a half kilos next session."))

	ctx := middleware.WithLogger(context.Background(), slog.New(slog.NewTextHandler(&buf, nil)))

	conversation, err := h.coach.StartConversation(ctx, h.user.ID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	stream, err := h.coach.SendMessage(ctx, h.user, conversation.ID, "how much should I add?")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, drainErr := drain(stream); drainErr != nil {
		t.Fatalf("drain: %v", drainErr)
	}
	waitForReply(t, h, conversation.ID, 2*time.Second)

	out := buf.String()
	if !strings.Contains(out, "ai generation") {
		t.Fatalf("no generation was logged: %s", out)
	}
	for _, want := range []string{"trace_id", "provider", "input_tokens", "output_tokens", "latency_ms"} {
		if !strings.Contains(out, want) {
			t.Errorf("generation log is missing %q; got %s", want, out)
		}
	}
}

// A conversation with a coach is the most sensitive thing North holds, and an
// observability change is exactly where it leaks by accident.
func TestAGenerationLogNeverCarriesMessageContent(t *testing.T) {
	var buf bytes.Buffer
	h := newHarness(t, fake.Text("SECRETREPLYTEXT about your knee"))

	ctx := middleware.WithLogger(context.Background(), slog.New(slog.NewTextHandler(&buf, nil)))

	conversation, err := h.coach.StartConversation(ctx, h.user.ID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	stream, err := h.coach.SendMessage(ctx, h.user, conversation.ID, "SECRETPROMPTTEXT my knee hurts")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, drainErr := drain(stream); drainErr != nil {
		t.Fatalf("drain: %v", drainErr)
	}
	waitForReply(t, h, conversation.ID, 2*time.Second)

	out := buf.String()
	for _, forbidden := range []string{"SECRETPROMPTTEXT", "SECRETREPLYTEXT"} {
		if strings.Contains(out, forbidden) {
			t.Errorf("message content reached the logs: %q appears in %s", forbidden, out)
		}
	}
}
