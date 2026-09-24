package anthropic_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
)

func TestThinkingFromAToolRoundIsSentBackWithTheCall(t *testing.T) {
	api, client := newFakeAPI(t,
		sse(evStart,
			`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"look it up"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-1"}}`,
			evStop0,
			`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"get_exercise","input":{}}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{}"}}`,
			`{"type":"content_block_stop","index":1}`,
			evMsgDelta("tool_use", 5), evMsgStop),
		textMessage("done"),
	)

	ch, err := client.Chat(context.Background(), ai.Request{Messages: []ai.Message{ai.UserText("squat?")}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	var calls []ai.ToolCall
	var state json.RawMessage
	for _, c := range collect(t, ch) {
		if len(c.ToolCalls) > 0 {
			calls, state = c.ToolCalls, c.ProviderState
		}
	}
	if len(state) == 0 {
		t.Fatal("the tool-call chunk carried no thinking state")
	}

	callMsg := ai.ToolCallMessage(calls)
	callMsg.ProviderState = state
	_, err = client.Generate(context.Background(), ai.Request{Messages: []ai.Message{
		ai.UserText("squat?"), callMsg,
		ai.ToolResultMessage([]ai.ToolResult{{ID: "toolu_1", Content: "Barbell Full Squat"}}),
	}})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	msgs := sentAt(t, api, 1)
	first := blocks(msgs[1])[0]
	if first["type"] != "thinking" || first["signature"] != "sig-1" || first["thinking"] != "look it up" {
		t.Errorf("assistant turn opens with %v, want the thinking block unchanged", first)
	}
	if _, ok := api.body(t, 1)["thinking"]; ok {
		t.Error("thinking was configured on a request that has its state")
	}
}

func TestAReplayedCallWithNoStateTurnsThinkingOff(t *testing.T) {
	api, client := newFakeAPI(t, textMessage("done"))
	_, err := client.Generate(context.Background(), ai.Request{Messages: []ai.Message{
		ai.UserText("log my check-in"),
		ai.ToolCallMessage([]ai.ToolCall{{ID: "toolu_2", Name: "create_check_in", Arguments: json.RawMessage(`{"mood":4}`)}}),
		ai.ToolResultMessage([]ai.ToolResult{{ID: "toolu_2", Content: "logged"}}),
	}})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	thinking, _ := api.body(t, 0)["thinking"].(map[string]any)
	if thinking["type"] != "disabled" {
		t.Errorf("thinking = %v, want disabled for a call whose thinking was not kept", api.body(t, 0)["thinking"])
	}
}

// Thinking is only owed back within the tool-use turn in progress. An earlier
// turn's tool round, stored without state, must not switch thinking off for
// every turn that follows it.
func TestAnEarlierTurnsToolRoundLeavesThinkingOn(t *testing.T) {
	api, client := newFakeAPI(t, textMessage("ok"))
	_, err := client.Generate(context.Background(), ai.Request{Messages: []ai.Message{
		ai.UserText("show me a squat"),
		ai.ToolCallMessage([]ai.ToolCall{{ID: "toolu_1", Name: "get_exercise", Arguments: json.RawMessage(`{}`)}}),
		ai.ToolResultMessage([]ai.ToolResult{{ID: "toolu_1", Content: "Barbell Full Squat"}}),
		{Role: ai.RoleModel, Parts: []ai.Part{ai.TextPart("Feet shoulder-width.")}},
		ai.UserText("thanks — and tomorrow?"),
	}})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if _, ok := api.body(t, 0)["thinking"]; ok {
		t.Errorf("thinking = %v, want the model's default for a new turn", api.body(t, 0)["thinking"])
	}
}
