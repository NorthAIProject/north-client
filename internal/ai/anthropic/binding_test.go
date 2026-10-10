package anthropic_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
)

// A turn that waited for approval is resumed with a different system prompt
// (the write it approved changed the context) and, when a file came with it,
// a different first message. Thinking replayed from that turn no longer
// matches the conversation it was made in, and the API refuses the whole
// request unless told to drop such blocks. Dropping them costs the model its
// earlier reasoning, not the conversation.
func TestManagedThinkingDropsReplayedBlocksThatNoLongerMatch(t *testing.T) {
	api, client := newManagedFakeAPI(t, textMessage("saved"))

	call := ai.ToolCallMessage([]ai.ToolCall{{ID: "toolu_1", Name: "import_plan_from_attachment", Arguments: json.RawMessage(`{}`)}})
	call.ProviderState = json.RawMessage(`[{"kind":"thinking","thinking":"import it","signature":"sig-1"},{"kind":"tool_use","tool_use_id":"toolu_1"}]`)
	_, err := client.Generate(context.Background(), ai.Request{
		System: "context after the import",
		Messages: []ai.Message{
			ai.UserText("make a plan from the pdf"), call,
			ai.ToolResultMessage([]ai.ToolResult{{ID: "toolu_1", Content: "Saved Plano"}}),
		},
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	thinking, _ := api.body(t, 0)["thinking"].(map[string]any)
	binding, _ := thinking["block_binding"].(map[string]any)
	if thinking["type"] != "adaptive" || binding["prefix_mismatch_behavior"] != "drop_block" {
		t.Errorf("thinking = %v, want adaptive with block_binding.prefix_mismatch_behavior drop_block", thinking)
	}
	if beta := strings.Join(api.headers[0].Values("Anthropic-Beta"), ","); !strings.Contains(beta, "thinking-binding-controls-2026-08-01") {
		t.Errorf("anthropic-beta = %q, want thinking-binding-controls-2026-08-01", beta)
	}
}

func TestChatSendsTheBindingBetaWithAdaptiveThinking(t *testing.T) {
	api, client := newManagedFakeAPI(t, sse(evStart,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hi"}}`,
		evStop0, evMsgDelta("end_turn", 1), evMsgStop))

	ch, err := client.Chat(context.Background(), ai.Request{Messages: []ai.Message{ai.UserText("hello")}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	collect(t, ch)

	if beta := strings.Join(api.headers[0].Values("Anthropic-Beta"), ","); !strings.Contains(beta, "thinking-binding-controls-2026-08-01") {
		t.Errorf("anthropic-beta = %q, want thinking-binding-controls-2026-08-01", beta)
	}
}
