package anthropic_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/anthropic"
)

// assertTimedOutWithUsage checks a stream's ending: the usage the provider had
// reported so far, then the deadline as the last chunk. Without the error a
// truncated reply reads as a finished one; without the usage the most expensive
// call in the ledger counts as nothing.
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
	if usage.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100, what message_start reported", usage.InputTokens)
	}
}

// The reader is keeping up and the provider goes quiet after its first word.
func TestAStallPastTheDeadlineEndsInAnErrorAndUsage(t *testing.T) {
	client := newStallingAPI(t, anthropic.Options{DefaultModel: "claude-opus-5"}, evStart, evTextStart, evText("Hel"))

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	ch, err := client.Chat(ctx, ai.Request{Messages: []ai.Message{ai.UserText("go")}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	chunks := collect(t, ch)
	assertTimedOutWithUsage(t, chunks)
	if chunks[0].Text != "Hel" {
		t.Errorf("first chunk = %+v, want the text that streamed", chunks[0])
	}
}

// The deadline passes while a chunk is waiting for the reader. With the context
// already done, a send that also selects on it can only give up.
func TestADeadlineThatPassesWhileAChunkWaitsStillEndsInAnErrorAndUsage(t *testing.T) {
	client := newStallingAPI(t, anthropic.Options{DefaultModel: "claude-opus-5"}, evStart, evTextStart, evText("Hel"))

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	ch, err := client.Chat(ctx, ai.Request{Messages: []ai.Message{ai.UserText("go")}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	<-ctx.Done()
	time.Sleep(50 * time.Millisecond)

	assertTimedOutWithUsage(t, collect(t, ch))
}

// The whole path the coach reads: the meter sits between it and the adapter, and
// the same select used to drop the error there too.
func TestAMeteredStallDeliversTheErrorAndRecordsTheUsage(t *testing.T) {
	inner := newStallingAPI(t, anthropic.Options{DefaultModel: "claude-opus-5"}, evStart, evTextStart, evText("Hel"))

	recorded := make(chan ai.Usage, 1)
	client := ai.Metered(inner, usageRecorder(recorded), false)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	ch, err := client.Chat(ctx, ai.Request{Messages: []ai.Message{ai.UserText("go")}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	<-ctx.Done()
	time.Sleep(50 * time.Millisecond)

	assertTimedOutWithUsage(t, collect(t, ch))

	select {
	case u := <-recorded:
		if u.InputTokens != 100 {
			t.Errorf("recorded InputTokens = %d, want 100", u.InputTokens)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stall was never recorded")
	}
}

type usageRecorder chan ai.Usage

func (r usageRecorder) Record(_ context.Context, _, _ string, usage ai.Usage, _ bool) { r <- usage }
