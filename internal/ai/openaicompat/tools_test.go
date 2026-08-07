package openaicompat

import (
	"encoding/json"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
)

// Streamed tool calls arrive in fragments: the id and name once, then the
// argument JSON a few characters at a time. Reassembling them is the part of
// this adapter most likely to be silently wrong — a dropped fragment produces
// argument JSON that still parses, just with a field missing.
func TestStreamedToolCallsAreReassembledInOrder(t *testing.T) {
	t.Parallel()

	accumulator := newToolCallAccumulator()

	frag := func(index int, id, name, args string) toolCallPayload {
		var p toolCallPayload
		p.Index, p.ID = index, id
		p.Function.Name, p.Function.Arguments = name, args
		return p
	}

	// Two calls, interleaved, exactly as a provider sends them.
	accumulator.add([]toolCallPayload{frag(0, "call_a", "search_exercises", `{"mus`)})
	accumulator.add([]toolCallPayload{frag(1, "call_b", "calculate_macros", `{"go`)})
	accumulator.add([]toolCallPayload{frag(0, "", "", `cle":"lats"}`)})
	accumulator.add([]toolCallPayload{frag(1, "", "", `al":"cutting"}`)})

	calls := accumulator.calls()
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2", len(calls))
	}

	if calls[0].ID != "call_a" || calls[0].Name != "search_exercises" {
		t.Errorf("first call = %+v, want call_a/search_exercises", calls[0])
	}
	if calls[1].ID != "call_b" || calls[1].Name != "calculate_macros" {
		t.Errorf("second call = %+v, want call_b/calculate_macros", calls[1])
	}

	var args struct {
		Muscle string `json:"muscle"`
	}
	if err := json.Unmarshal(calls[0].Arguments, &args); err != nil {
		t.Fatalf("reassembled arguments do not parse: %v (%s)", err, calls[0].Arguments)
	}
	if args.Muscle != "lats" {
		t.Errorf("muscle = %q, want lats", args.Muscle)
	}
}

// A call with no arguments must still produce parseable JSON, so every caller
// can unmarshal without a nil check first.
func TestACallWithNoArgumentsBecomesAnEmptyObject(t *testing.T) {
	t.Parallel()

	accumulator := newToolCallAccumulator()

	var fragment toolCallPayload
	fragment.ID, fragment.Function.Name = "call_a", "weekly_review"
	accumulator.add([]toolCallPayload{fragment})

	calls := accumulator.calls()
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if string(calls[0].Arguments) != "{}" {
		t.Errorf("Arguments = %s, want {}", calls[0].Arguments)
	}
}

func TestNoToolCallsProducesNothing(t *testing.T) {
	t.Parallel()

	if calls := newToolCallAccumulator().calls(); calls != nil {
		t.Errorf("got %v, want nil when nothing was streamed", calls)
	}
}

// Tool results become their own "tool" messages, and the assistant turn that
// asked for them has to come first — every OpenAI-compatible API rejects a
// result whose call it has not seen.
func TestToolTurnsAreRenderedInTheOpenAIShape(t *testing.T) {
	t.Parallel()

	client := &Client{defaultModel: "test-model"}

	body := client.body(ai.Request{
		Messages: []ai.Message{
			ai.UserText("how many calories should I eat?"),
			ai.ToolCallMessage([]ai.ToolCall{{ID: "call_a", Name: "calculate_macros", Arguments: json.RawMessage(`{"goal":"cutting"}`)}}),
			ai.ToolResultMessage([]ai.ToolResult{{ID: "call_a", Name: "calculate_macros", Content: "2100 kcal"}}),
		},
		Tools: []ai.Tool{{
			Name:        "calculate_macros",
			Description: "Work out a calorie and macro target.",
			Parameters:  ai.Object("arguments", map[string]*ai.Schema{"goal": ai.String("the objective")}, "goal"),
		}},
	}, false)

	messages, ok := body["messages"].([]map[string]any)
	if !ok {
		t.Fatalf("messages has type %T", body["messages"])
	}
	if len(messages) != 3 {
		t.Fatalf("got %d messages, want 3", len(messages))
	}

	if role := messages[1]["role"]; role != "assistant" {
		t.Errorf("the call turn has role %v, want assistant", role)
	}
	if messages[1]["tool_calls"] == nil {
		t.Error("the call turn carries no tool_calls")
	}

	if role := messages[2]["role"]; role != "tool" {
		t.Errorf("the result turn has role %v, want tool", role)
	}
	if id := messages[2]["tool_call_id"]; id != "call_a" {
		t.Errorf("tool_call_id = %v, want call_a — a result whose call cannot be matched is rejected", id)
	}

	tools, ok := body["tools"].([]map[string]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %v, want one declaration", body["tools"])
	}
	if tools[0]["type"] != "function" {
		t.Errorf("tool type = %v, want function", tools[0]["type"])
	}
}

// Tools absent must leave the request exactly as it was before tool support
// existed, or every current call site changes behaviour by being recompiled.
func TestARequestWithoutToolsCarriesNoToolFields(t *testing.T) {
	t.Parallel()

	client := &Client{defaultModel: "test-model"}
	body := client.body(ai.Request{Messages: []ai.Message{ai.UserText("hello")}}, false)

	if _, present := body["tools"]; present {
		t.Error("a request with no tools should not carry a tools field")
	}

	messages := body["messages"].([]map[string]any)
	if len(messages) != 1 {
		t.Fatalf("got %d messages, want 1", len(messages))
	}
	if _, present := messages[0]["tool_calls"]; present {
		t.Error("a plain turn should not carry tool_calls")
	}
}

func TestFromToolCallPayloadDefaultsEmptyArguments(t *testing.T) {
	t.Parallel()

	var payload toolCallPayload
	payload.ID, payload.Function.Name = "call_a", "weekly_review"

	calls := fromToolCallPayload([]toolCallPayload{payload})
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(calls))
	}
	if string(calls[0].Arguments) != "{}" {
		t.Errorf("Arguments = %s, want {}", calls[0].Arguments)
	}
}
