package anthropic_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/anthropic"
)

// One real tool round trip, the shape every coach turn about a movement takes.
// Skipped without a key, so it costs nothing in CI.
func TestLiveToolRoundTrip(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY not set; skipping live test")
	}
	client, err := anthropic.New(anthropic.Options{APIKey: key})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	tool := ai.Tool{
		Name: "get_exercise", Description: "Read one exercise from the catalogue. Call it before describing a movement.",
		Parameters: ai.Object("which", map[string]*ai.Schema{"slug": ai.String("the exercise slug")}, "slug"),
	}
	req := ai.Request{
		System:   "You are a coach. Always look an exercise up before describing it.",
		Messages: []ai.Message{ai.UserText("How do I do a barbell-full-squat?")},
		Tools:    []ai.Tool{tool},
	}

	calls, state := liveRound(t, ctx, client, req)
	if len(calls) == 0 {
		t.Fatal("the model did not call the tool")
	}
	callMsg := ai.ToolCallMessage(calls)
	callMsg.ProviderState = state
	req.Messages = append(req.Messages, callMsg, ai.ToolResultMessage([]ai.ToolResult{{
		ID: calls[0].ID, Name: calls[0].Name, Content: "Barbell Full Squat: bar on upper back, sit back and down, drive up.",
	}}))

	calls, _ = liveRound(t, ctx, client, req)
	if len(calls) != 0 {
		t.Logf("model asked again: %s", mustJSON(calls))
	}
}

func liveRound(t *testing.T, ctx context.Context, client *anthropic.Client, req ai.Request) ([]ai.ToolCall, json.RawMessage) {
	t.Helper()
	ch, err := client.Chat(ctx, req)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	var calls []ai.ToolCall
	var state json.RawMessage
	for c := range ch {
		if c.Err != nil {
			t.Fatalf("stream: %v", c.Err)
		}
		if len(c.ToolCalls) > 0 {
			calls, state = c.ToolCalls, c.ProviderState
		}
		if c.Usage != nil {
			t.Logf("usage: %d in, %d out, model %s", c.Usage.InputTokens, c.Usage.OutputTokens, c.Model)
		}
	}
	return calls, state
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
