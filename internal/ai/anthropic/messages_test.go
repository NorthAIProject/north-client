package anthropic_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
)

func sent(t *testing.T, api *fakeAPI) []map[string]any {
	t.Helper()
	raw, _ := api.body(t, 0)["messages"].([]any)
	out := make([]map[string]any, len(raw))
	for i, m := range raw {
		out[i] = m.(map[string]any)
	}
	return out
}

func blocks(m map[string]any) []map[string]any {
	raw, _ := m["content"].([]any)
	out := make([]map[string]any, len(raw))
	for i, b := range raw {
		out[i] = b.(map[string]any)
	}
	return out
}

func TestAToolRoundTripIsSentInAnthropicsShape(t *testing.T) {
	api, client := newFakeAPI(t, textMessage("Feet shoulder-width."))

	_, err := client.Generate(context.Background(), ai.Request{
		Messages: []ai.Message{
			ai.UserText("show me a squat"),
			ai.ToolCallMessage([]ai.ToolCall{{ID: "toolu_1", Name: "get_exercise", Arguments: json.RawMessage(`{"slug":"barbell-full-squat"}`)}}),
			ai.ToolResultMessage([]ai.ToolResult{
				{ID: "toolu_1", Name: "get_exercise", Content: "Barbell Full Squat"},
			}),
		},
		Tools: []ai.Tool{{
			Name: "get_exercise", Description: "Read one exercise.",
			Parameters: ai.Object("which", map[string]*ai.Schema{"slug": ai.String("slug")}, "slug"),
		}},
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	msgs := sent(t, api)
	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want 3", len(msgs))
	}
	use := blocks(msgs[1])[0]
	if msgs[1]["role"] != "assistant" || use["type"] != "tool_use" || use["id"] != "toolu_1" || use["name"] != "get_exercise" {
		t.Errorf("tool call sent as %v", msgs[1])
	}
	if input, _ := use["input"].(map[string]any); input["slug"] != "barbell-full-squat" {
		t.Errorf("tool input = %v", use["input"])
	}
	result := blocks(msgs[2])[0]
	if msgs[2]["role"] != "user" || result["type"] != "tool_result" || result["tool_use_id"] != "toolu_1" {
		t.Errorf("tool result sent as %v", msgs[2])
	}

	tools, _ := api.body(t, 0)["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %v", api.body(t, 0)["tools"])
	}
	tool := tools[0].(map[string]any)
	schema, _ := tool["input_schema"].(map[string]any)
	if tool["name"] != "get_exercise" || schema["type"] != "object" {
		t.Errorf("tool = %v", tool)
	}
	if required, _ := schema["required"].([]any); len(required) != 1 || required[0] != "slug" {
		t.Errorf("required = %v, want [slug]", schema["required"])
	}
}

func TestAllResultsOfOneRoundGoInOneUserMessage(t *testing.T) {
	api, client := newFakeAPI(t, textMessage("ok"))
	_, err := client.Generate(context.Background(), ai.Request{Messages: []ai.Message{
		ai.UserText("two things"),
		ai.ToolCallMessage([]ai.ToolCall{
			{ID: "a", Name: "x", Arguments: json.RawMessage(`{}`)},
			{ID: "b", Name: "y", Arguments: json.RawMessage(`{}`)},
		}),
		ai.ToolResultMessage([]ai.ToolResult{{ID: "a", Content: "1"}, {ID: "b", Content: "no", IsError: true}}),
	}})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	results := blocks(sent(t, api)[2])
	if len(results) != 2 || results[1]["is_error"] != true {
		t.Errorf("results = %v, want both, the second marked as an error", results)
	}
}

func TestSameRoleMessagesAreMergedAndEmptyTextIsDropped(t *testing.T) {
	api, client := newFakeAPI(t, textMessage("ok"))
	_, err := client.Generate(context.Background(), ai.Request{Messages: []ai.Message{
		ai.UserText("first"),
		ai.UserText("second"),
		{Role: ai.RoleModel, Parts: []ai.Part{ai.TextPart("")}},
		ai.UserText("third"),
	}})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	msgs := sent(t, api)
	if len(msgs) != 1 || len(blocks(msgs[0])) != 3 {
		t.Errorf("messages = %v, want one user message with three text blocks", msgs)
	}
}

func TestAThreadThatBeginsWithKhepriStillStartsWithAUserTurn(t *testing.T) {
	api, client := newFakeAPI(t, textMessage("ok"))
	_, err := client.Generate(context.Background(), ai.Request{Messages: []ai.Message{
		{Role: ai.RoleModel, Parts: []ai.Part{ai.TextPart("Good morning — today is Upper B.")}},
		ai.UserText("thanks"),
	}})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	msgs := sent(t, api)
	if len(msgs) != 3 || msgs[0]["role"] != "user" || msgs[1]["role"] != "assistant" {
		t.Errorf("messages = %v, want a user placeholder before the briefing", msgs)
	}
}

func TestAToolCallInTheReplyIsReturned(t *testing.T) {
	b, _ := json.Marshal(map[string]any{
		"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-5",
		"content": []any{map[string]any{
			"type": "tool_use", "id": "toolu_9", "name": "get_exercise",
			"input": map[string]any{"slug": "squat"},
		}},
		"stop_reason": "tool_use",
		"usage":       map[string]any{"input_tokens": 1, "output_tokens": 1},
	})
	_, client := newFakeAPI(t, cannedResponse{body: string(b)})

	resp, err := client.Generate(context.Background(), ai.Request{Messages: []ai.Message{ai.UserText("squat?")}})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "toolu_9" || resp.ToolCalls[0].Name != "get_exercise" {
		t.Fatalf("tool calls = %+v", resp.ToolCalls)
	}
	var args map[string]string
	if err := json.Unmarshal(resp.ToolCalls[0].Arguments, &args); err != nil || args["slug"] != "squat" {
		t.Errorf("arguments = %s", resp.ToolCalls[0].Arguments)
	}
}
