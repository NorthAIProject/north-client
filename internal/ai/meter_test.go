package ai_test

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai"
)

// meterStub answers Chat and Generate with fixed usage. onGenerate lets a test
// act as the caller at the moment the call returns.
type meterStub struct {
	stub
	onGenerate func()
}

func (s meterStub) Chat(context.Context, ai.Request) (<-chan ai.StreamChunk, error) {
	out := make(chan ai.StreamChunk, 2)
	out <- ai.StreamChunk{Text: "hello"}
	out <- ai.StreamChunk{Usage: &ai.Usage{InputTokens: 10, OutputTokens: 5}, Model: "m"}
	close(out)
	return out, nil
}

func (s meterStub) Generate(context.Context, ai.Request) (*ai.Response, error) {
	if s.onGenerate != nil {
		s.onGenerate()
	}
	return &ai.Response{Text: "hello", Model: "m", Usage: ai.Usage{InputTokens: 10, OutputTokens: 5}}, nil
}

// meterFunc adapts a function to ai.Meter.
type meterFunc func(ctx context.Context)

func (f meterFunc) Record(ctx context.Context, _, _ string, _ ai.Usage, _ bool) { f(ctx) }

type attrKey struct{}

// A saturated pool must not hold the end of a reply hostage: the reader is told
// the stream is over before the spend row is written.
func TestAStreamClosesWithoutWaitingForASlowMeter(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })

	entered := make(chan struct{})
	slow := meterFunc(func(context.Context) {
		close(entered)
		select {
		case <-release:
		case <-time.After(5 * time.Second):
		}
	})

	client := ai.Metered(meterStub{stub: stub{name: "alpha"}}, slow, false)
	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}

	start := time.Now()
	chunks := 0
	for range stream {
		chunks++
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("the stream took %s to close behind a meter that blocks for 5s", elapsed)
	}
	if chunks != 2 {
		t.Errorf("saw %d chunks, want 2", chunks)
	}

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the meter was never called")
	}
}

// The recorder gets a bound of its own: the caller's cancellation is stripped,
// so a row is not lost to a closed tab, and the deadline that comes with it
// stops a stuck database from holding the goroutine for ever.
func TestAStreamIsRecordedOnADetachedBoundedContext(t *testing.T) {
	recorded := make(chan struct{})
	client := ai.Metered(meterStub{stub: stub{name: "alpha"}}, meterFunc(func(ctx context.Context) {
		defer close(recorded)
		assertDetachedAndBounded(t, ctx)
	}), false)

	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), attrKey{}, "user"))
	stream, err := client.Chat(ctx, ai.Request{})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	cancel()
	for range stream {
	}

	select {
	case <-recorded:
	case <-time.After(5 * time.Second):
		t.Fatal("the meter was never called")
	}
}

// The recorder runs on the meter's own goroutine, with no handler above it. The
// reply has already been served by then, so its failure must not end the process.
func TestAPanickingMeterSparesTheProcess(t *testing.T) {
	entered := make(chan struct{})
	client := ai.Metered(meterStub{stub: stub{name: "alpha"}}, meterFunc(func(context.Context) {
		close(entered)
		panic("boom")
	}), false)

	stream, err := client.Chat(context.Background(), ai.Request{})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	for range stream {
	}

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the meter was never called")
	}
	// Long enough for an unrecovered panic to take the test binary with it.
	time.Sleep(50 * time.Millisecond)
}

// A caller that cancels as soon as Generate returns has still spent the tokens.
func TestGenerateIsRecordedAfterTheCallerCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), attrKey{}, "user"))
	defer cancel()

	recorded := false
	client := ai.Metered(
		meterStub{stub: stub{name: "alpha"}, onGenerate: cancel},
		meterFunc(func(ctx context.Context) {
			recorded = true
			assertDetachedAndBounded(t, ctx)
		}),
		false,
	)

	if _, err := client.Generate(ctx, ai.Request{}); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !recorded {
		t.Fatal("the meter was never called")
	}
}

func assertDetachedAndBounded(t *testing.T, ctx context.Context) {
	t.Helper()
	if err := ctx.Err(); err != nil {
		t.Errorf("the recorder's context is already done (%v); the row would be lost", err)
	}
	if ctx.Value(attrKey{}) != "user" {
		t.Error("the recorder's context lost its values; the row would be unattributed")
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Error("the recorder's context has no deadline; a saturated pool could hold it for ever")
		return
	}
	if remaining := time.Until(deadline); remaining > 2*time.Second {
		t.Errorf("the recorder has %s, want at most 2s", remaining)
	}
}
