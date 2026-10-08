package anthropic_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
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

const (
	evThinkingStart = `{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`
	evThinking      = `{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"hmm"}}`
	evSignature     = `{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-1"}}`
	evText1Start    = `{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`
	evStop1         = `{"type":"content_block_stop","index":1}`
)

func evText1(text string) string {
	return fmt.Sprintf(`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":%q}}`, text)
}

// Before any text has streamed nothing has reached the person, so a failure
// there is Chat's own error. That is what lets the runner ask the next
// provider instead of leaving the coach with a broken reply.
func TestChatReturnsAFailureBeforeTheReplyCommits(t *testing.T) {
	overloaded := apiError(529, "overloaded_error", "Overloaded")
	cases := map[string]struct {
		res  []cannedResponse
		want error
	}{
		"http error": {[]cannedResponse{overloaded, overloaded, overloaded, overloaded}, apperr.ErrUnavailable},
		"spend limit": {[]cannedResponse{apiError(400, "invalid_request_error",
			"You have reached your specified workspace API usage limits.")}, apperr.ErrPaymentRequired},
		"refusal": {[]cannedResponse{sse(evStart, evMsgDelta("refusal", 0), evMsgStop)}, apperr.ErrUnavailable},
		"thinking used every token": {[]cannedResponse{sse(evStart, evThinkingStart, evThinking, evSignature, evStop0,
			evMsgDelta("max_tokens", 1024), evMsgStop)}, apperr.ErrUnavailable},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, client := newManagedFakeAPI(t, tc.res...)
			ch, err := client.Chat(context.Background(), ai.Request{Messages: []ai.Message{ai.UserText("squat?")}})
			if ch != nil {
				t.Error("a channel was returned alongside the error")
			}
			if !apperr.Is(err, tc.want) || !ai.Failover(err) {
				t.Fatalf("err = %v, want %v so the runner fails over", err, tc.want)
			}
		})
	}
}

func TestChatHoldsThinkingBackThenStreamsTheText(t *testing.T) {
	_, client := newManagedFakeAPI(t, sse(evStart, evThinkingStart, evThinking, evSignature, evStop0,
		evText1Start, evText1("Sit "), evText1("back."), evStop1, evMsgDelta("end_turn", 9), evMsgStop))

	ch, err := client.Chat(context.Background(), ai.Request{Messages: []ai.Message{ai.UserText("squat?")}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	var text strings.Builder
	var last ai.StreamChunk
	for _, c := range collect(t, ch) {
		if c.Err != nil {
			t.Fatalf("stream error: %v", c.Err)
		}
		text.WriteString(c.Text)
		last = c
	}
	if text.String() != "Sit back." {
		t.Errorf("text = %q; the first delta read before Chat returned must not be lost", text.String())
	}
	if last.Usage == nil || last.Usage.OutputTokens != 9 {
		t.Errorf("last chunk = %+v, want usage", last)
	}
}

// Once text has gone out, a refusal can only arrive on the channel: Chat has
// already returned.
func TestARefusalAfterTextArrivesOnTheChannel(t *testing.T) {
	_, client := newFakeAPI(t, sse(evStart, evTextStart, evText("Sure, "), evStop0, evMsgDelta("refusal", 3), evMsgStop))

	ch, err := client.Chat(context.Background(), ai.Request{Messages: []ai.Message{ai.UserText("go")}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	var gotErr error
	for _, c := range collect(t, ch) {
		if c.Err != nil {
			gotErr = c.Err
		}
	}
	if !apperr.Is(gotErr, apperr.ErrUnavailable) {
		t.Fatalf("stream err = %v, want ErrUnavailable", gotErr)
	}
}
