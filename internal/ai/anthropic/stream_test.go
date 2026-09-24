package anthropic_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai"
)

// sse renders events the way the Messages API streams them.
func sse(events ...string) cannedResponse {
	var b strings.Builder
	for _, e := range events {
		var probe struct{ Type string }
		_ = jsonUnmarshal(e, &probe)
		fmt.Fprintf(&b, "event: %s\ndata: %s\n\n", probe.Type, e)
	}
	return cannedResponse{body: b.String(), stream: true}
}

const (
	evStart     = `{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5","content":[],"stop_reason":null,"usage":{"input_tokens":10,"output_tokens":1,"cache_read_input_tokens":90}}}`
	evTextStart = `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`
	evStop0     = `{"type":"content_block_stop","index":0}`
	evMsgStop   = `{"type":"message_stop"}`
)

func evText(text string) string {
	return fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%q}}`, text)
}

func evMsgDelta(stop string, out int) string {
	return fmt.Sprintf(`{"type":"message_delta","delta":{"stop_reason":%q},"usage":{"output_tokens":%d}}`, stop, out)
}

func collect(t *testing.T, ch <-chan ai.StreamChunk) []ai.StreamChunk {
	t.Helper()
	var out []ai.StreamChunk
	timeout := time.After(5 * time.Second)
	for {
		select {
		case c, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, c)
		case <-timeout:
			t.Fatal("stream never closed")
		}
	}
}

func TestChatStreamsTextThenUsage(t *testing.T) {
	_, client := newFakeAPI(t, sse(evStart, evTextStart, evText("Sit "), evText("back."), evStop0,
		evMsgDelta("end_turn", 7), evMsgStop))

	ch, err := client.Chat(context.Background(), ai.Request{Messages: []ai.Message{ai.UserText("squat?")}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	chunks := collect(t, ch)

	var text strings.Builder
	var last ai.StreamChunk
	for _, c := range chunks {
		if c.Err != nil {
			t.Fatalf("stream error: %v", c.Err)
		}
		text.WriteString(c.Text)
		last = c
	}
	if text.String() != "Sit back." {
		t.Errorf("text = %q", text.String())
	}
	if last.Usage == nil || last.Usage.InputTokens != 100 || last.Usage.OutputTokens != 7 || last.Model != "claude-opus-5" {
		t.Errorf("last chunk = %+v, want usage 100 in / 7 out and the model", last)
	}
}

func TestChatHandsBackAToolCallInOneChunk(t *testing.T) {
	_, client := newFakeAPI(t, sse(evStart,
		`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"get_exercise","input":{}}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"slug\":"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"squat\"}"}}`,
		evStop0, evMsgDelta("tool_use", 12), evMsgStop))

	ch, err := client.Chat(context.Background(), ai.Request{Messages: []ai.Message{ai.UserText("squat?")}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	var calls []ai.ToolCall
	for _, c := range collect(t, ch) {
		if c.Err != nil {
			t.Fatalf("stream error: %v", c.Err)
		}
		if len(c.ToolCalls) > 0 {
			if calls != nil {
				t.Fatal("tool calls arrived in more than one chunk")
			}
			calls = c.ToolCalls
		}
	}
	if len(calls) != 1 || calls[0].ID != "toolu_1" || string(calls[0].Arguments) != `{"slug":"squat"}` {
		t.Errorf("calls = %+v", calls)
	}
}

func TestAnAbandonedStreamClosesWithoutBlocking(t *testing.T) {
	events := []string{evStart, evTextStart}
	for i := 0; i < 200; i++ {
		events = append(events, evText("word "))
	}
	events = append(events, evStop0, evMsgDelta("end_turn", 200), evMsgStop)
	_, client := newFakeAPI(t, sse(events...))

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := client.Chat(ctx, ai.Request{Messages: []ai.Message{ai.UserText("go")}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	<-ch // read one chunk, then walk away
	cancel()

	done := make(chan struct{})
	go func() {
		for range ch {
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the stream did not close after the caller went away")
	}
}
