package openaicompat_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai"
)

// stallingStream writes frames, flushes, and goes quiet until the client gives
// up. The usage frame comes first on purpose: this dialect normally sends it
// last, so a stream cut short has no usage to report unless a provider sent some
// early. These tests cover delivering what arrived, and cannot cover more.
func stallingStream(release <-chan struct{}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `data: {"model":"m","choices":[],"usage":{"prompt_tokens":7,"completion_tokens":1}}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"Hel"}}]}`+"\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}
}

func drainWithin(t *testing.T, ch <-chan ai.StreamChunk) []ai.StreamChunk {
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

func assertTimedOutWithUsage(t *testing.T, chunks []ai.StreamChunk) {
	t.Helper()
	if len(chunks) == 0 {
		t.Fatal("the stream closed with nothing in it")
	}
	last := chunks[len(chunks)-1]
	if !errors.Is(last.Err, context.DeadlineExceeded) {
		t.Errorf("last chunk error = %v, want the deadline", last.Err)
	}

	var usage *ai.Usage
	for _, c := range chunks[:len(chunks)-1] {
		if c.Err != nil {
			t.Errorf("an error came before the end of the stream: %v", c.Err)
		}
		if c.Usage != nil {
			usage = c.Usage
		}
	}
	if usage == nil {
		t.Fatal("no usage was reported before the error")
	}
	if usage.InputTokens != 7 {
		t.Errorf("InputTokens = %d, want 7", usage.InputTokens)
	}
}

// The reader is keeping up and the provider goes quiet after its first word.
func TestAStallPastTheDeadlineEndsInAnErrorAndUsage(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	c, _ := newClient(t, stallingStream(release))

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	ch, err := c.Chat(ctx, ai.Request{Messages: []ai.Message{ai.UserText("go")}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	assertTimedOutWithUsage(t, drainWithin(t, ch))
}

// The deadline passes while a chunk is waiting for the reader. With the context
// already done, a send that also selects on it can only give up.
func TestADeadlineThatPassesWhileAChunkWaitsStillEndsInAnErrorAndUsage(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	c, _ := newClient(t, stallingStream(release))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	ch, err := c.Chat(ctx, ai.Request{Messages: []ai.Message{ai.UserText("go")}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	<-ctx.Done()
	time.Sleep(50 * time.Millisecond)

	assertTimedOutWithUsage(t, drainWithin(t, ch))
}
