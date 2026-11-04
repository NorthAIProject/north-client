package anthropic_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/anthropic"
	"github.com/NorthAIProject/north-client/internal/ai/openaicompat"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

const testBudget = 150 * time.Millisecond

var goRequest = ai.Request{Messages: []ai.Message{ai.UserText("go")}}

// newRateLimitedAPI refuses every request with a 429 and asks for a wait far
// longer than a person should sit through, and counts how often it was asked.
func newRateLimitedAPI(t *testing.T, retryAfter string, opts anthropic.Options) (*anthropic.Client, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", retryAfter)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`)
	}))
	t.Cleanup(srv.Close)

	opts.APIKey = "sk-ant-test"
	opts.BaseURL = srv.URL
	opts.HTTPClient = srv.Client()
	client, err := anthropic.New(opts)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return client, &hits
}

// The chain is a managed client's retry. Sleeping out a Retry-After in front of
// a provider that could answer now is the delay this exists to remove.
func TestAManagedClientDoesNotRetryARateLimit(t *testing.T) {
	calls := map[string]func(*anthropic.Client, context.Context) error{
		"chat": func(c *anthropic.Client, ctx context.Context) error {
			_, err := c.Chat(ctx, goRequest)
			return err
		},
		"generate": func(c *anthropic.Client, ctx context.Context) error {
			_, err := c.Generate(ctx, goRequest)
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			client, hits := newRateLimitedAPI(t, "30", anthropic.Options{Managed: true})

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			start := time.Now()
			err := call(client, ctx)

			if !apperr.Is(err, apperr.ErrUnavailable) {
				t.Errorf("err = %v, want ErrUnavailable", err)
			}
			if !ai.Failover(err) {
				t.Errorf("err = %v does not fail over", err)
			}
			if got := hits.Load(); got != 1 {
				t.Errorf("the API was hit %d times, want exactly 1", got)
			}
			if elapsed := time.Since(start); elapsed > time.Second {
				t.Errorf("took %s to give up", elapsed)
			}
		})
	}
}

// A person's own key is the only provider they have asked for, and it keeps
// what the SDK does by default.
func TestAPersonalKeyKeepsTheSDKRetries(t *testing.T) {
	client, hits := newRateLimitedAPI(t, "0", anthropic.Options{})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := client.Chat(ctx, goRequest); !apperr.Is(err, apperr.ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
	if got := hits.Load(); got != 3 {
		t.Errorf("the API was hit %d times, want 3, the SDK's default", got)
	}
}

// newGoodProvider is the next rung of a chain: a provider that simply answers.
func newGoodProvider(t *testing.T) ai.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"from the second"}}]}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	client, err := openaicompat.New(openaicompat.Options{
		Name: "second", BaseURL: srv.URL, APIKey: "test-key", DefaultModel: "m", HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return client
}

// A provider that accepts the request and says nothing has to cost one budget,
// not the whole generation, however far it got before going quiet.
func TestAStallBeforeTheFirstTokenFailsOverToTheNextProvider(t *testing.T) {
	stalls := map[string][]string{
		"before any event": nil,
		"while thinking":   {evStart, evThinkingStart, evThinking},
	}
	for name, events := range stalls {
		t.Run(name, func(t *testing.T) {
			first := newStallingAPI(t, anthropic.Options{Managed: true, FirstTokenBudget: testBudget}, events...)

			registry := ai.NewRegistry()
			registry.Register(first)
			registry.Register(newGoodProvider(t))
			runner := ai.NewRunner(registry, ai.NewChainSet([]string{"anthropic", "second"}, nil))

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			start := time.Now()
			var stream <-chan ai.StreamChunk
			answered, err := runner.Run(ctx, ai.RunOptions{}, func(c ai.Client) error {
				opened, err := c.Chat(ctx, goRequest)
				stream = opened
				return err
			})
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			if answered.Name() != "second" {
				t.Fatalf("answered by %q, want the second provider", answered.Name())
			}

			var reply strings.Builder
			for chunk := range stream {
				reply.WriteString(chunk.Text)
			}
			if reply.String() != "from the second" {
				t.Errorf("reply = %q", reply.String())
			}
			if elapsed := time.Since(start); elapsed > testBudget+time.Second {
				t.Errorf("took %s for a budget of %s", elapsed, testBudget)
			}
		})
	}
}

// The budget's own cancellation must not read as the caller's. Failover refuses
// anything carrying context.Canceled, so wrapping it would stop the chain here.
func TestAnExhaustedBudgetIsNotACancellation(t *testing.T) {
	client := newStallingAPI(t, anthropic.Options{Managed: true, FirstTokenBudget: testBudget})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	_, err := client.Chat(ctx, goRequest)

	if !apperr.Is(err, apperr.ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v carries a context error, so Failover refuses it", err)
	}
	if !ai.Failover(err) {
		t.Errorf("err = %v does not fail over", err)
	}
}

// The caller going away is the caller's, budget or no budget. Spending the rest
// of the chain on nobody is what Failover's guard is there to stop.
func TestACallerThatGoesAwayDoesNotFailOver(t *testing.T) {
	cases := map[string]func() (context.Context, context.CancelFunc){
		"cancelled": func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(testBudget/3, cancel)
			return ctx, cancel
		},
		"deadline": func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), testBudget/3)
		},
	}
	for name, parent := range cases {
		t.Run(name, func(t *testing.T) {
			client := newStallingAPI(t, anthropic.Options{Managed: true, FirstTokenBudget: testBudget})

			ctx, cancel := parent()
			defer cancel()

			_, err := client.Chat(ctx, goRequest)
			if err == nil {
				t.Fatal("chat succeeded against a provider that never answered")
			}
			if ai.Failover(err) {
				t.Errorf("err = %v fails over for a caller that has gone", err)
			}
		})
	}
}

// Once text has reached the person, a slow patch is the model thinking between
// sentences. Cutting it off would leave half an answer with nowhere to go.
func TestTheBudgetStopsOnceStreamingStarts(t *testing.T) {
	first := sse(evStart, evTextStart, evText("Hel")).body
	rest := sse(evText("lo"), evStop0, evMsgDelta("end_turn", 2), evMsgStop).body

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, first)
		w.(http.Flusher).Flush()
		time.Sleep(3 * testBudget)
		_, _ = io.WriteString(w, rest)
	}))
	t.Cleanup(srv.Close)

	client, err := anthropic.New(anthropic.Options{
		APIKey: "sk-ant-test", BaseURL: srv.URL, HTTPClient: srv.Client(),
		Managed: true, FirstTokenBudget: testBudget,
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	ch, err := client.Chat(context.Background(), goRequest)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}

	var reply strings.Builder
	for _, chunk := range collect(t, ch) {
		if chunk.Err != nil {
			t.Errorf("stream error: %v", chunk.Err)
		}
		reply.WriteString(chunk.Text)
	}
	if reply.String() != "Hello" {
		t.Errorf("reply = %q, want the whole of it", reply.String())
	}
}

// The budget is a managed client's. A person's own provider has no chain behind
// it that it answers for, and the override must not sneak one in.
func TestAPersonalKeyHasNoBudget(t *testing.T) {
	client := newStallingAPI(t, anthropic.Options{FirstTokenBudget: testBudget})

	ctx, cancel := context.WithTimeout(context.Background(), 4*testBudget)
	defer cancel()

	_, err := client.Chat(ctx, goRequest)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want the caller's own deadline", err)
	}
}

func TestAManagedClientDefaultsToTheFirstTokenBudget(t *testing.T) {
	managed, err := anthropic.New(anthropic.Options{APIKey: "sk-ant-test", Managed: true})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if got := anthropic.ClientFirstTokenBudget(managed); got != 45*time.Second {
		t.Errorf("managed budget = %s, want 45s", got)
	}

	personal, err := anthropic.New(anthropic.Options{APIKey: "sk-ant-test"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	if got := anthropic.ClientFirstTokenBudget(personal); got != 0 {
		t.Errorf("personal budget = %s, want none", got)
	}
}
