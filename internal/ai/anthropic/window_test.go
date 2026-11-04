package anthropic_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/conversations"
)

// The whole way from a stored window to the wire: a window cut in the middle of
// a tool turn must not reach the API as a tool_result with no tool_use before
// it, which is a 400.
func TestAWindowCutInsideAToolTurnIsSentWithoutAnOrphan(t *testing.T) {
	api, client := newFakeAPI(t, textMessage("Keep going."))

	window := []conversations.Message{
		{Role: ai.RoleUser, ToolResults: []ai.ToolResult{{ID: "toolu_gone", Name: "get_goals", Content: "2 goals"}}},
		{Role: ai.RoleModel, ToolCalls: []ai.ToolCall{{ID: "toolu_lost", Name: "get_goals", Arguments: json.RawMessage(`{}`)}}},
		{Role: ai.RoleUser, ToolResults: []ai.ToolResult{{ID: "toolu_lost", Name: "get_goals", Content: "2 goals"}}},
		{Role: ai.RoleModel, Content: "You have two goals."},
		{Role: ai.RoleUser, Content: "and my sleep?"},
		{Role: ai.RoleModel, ToolCalls: []ai.ToolCall{{ID: "toolu_kept", Name: "get_sleep", Arguments: json.RawMessage(`{}`)}}},
		{Role: ai.RoleUser, ToolResults: []ai.ToolResult{{ID: "toolu_kept", Name: "get_sleep", Content: "7 hours"}}},
	}

	if _, err := client.Generate(context.Background(), ai.Request{Messages: conversations.ToAIMessages(window)}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	msgs := sent(t, api)
	shown := map[string]bool{}
	for i, m := range msgs {
		for _, b := range blocks(m) {
			switch b["type"] {
			case "tool_use":
				shown[b["id"].(string)] = true
			case "tool_result":
				if id := b["tool_use_id"].(string); !shown[id] {
					t.Errorf("message %d answers %s, which the API was never shown", i, id)
				}
			}
		}
	}
	if !shown["toolu_kept"] {
		t.Errorf("the turn being answered was dropped: %v", msgs)
	}
	if shown["toolu_lost"] {
		t.Errorf("a pair with no question before it was kept: %v", msgs)
	}
}
