package gemini_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/ai/gemini"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

var goRequest = ai.Request{Messages: []ai.Message{ai.UserText("go")}}

const (
	frameHel = `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"Hel"}]}}]}` + "\n\n"
	frameLo  = `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"lo"}]}}],` +
		`"modelVersion":"gemini-test-001","usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":2}}` + "\n\n"
	frameErr = `data: {"error":{"code":503,"message":"overloaded","status":"UNAVAILABLE"}}` + "\n\n"
)

// newClient points a real client at a stub, so no key and no network are used.
func newClient(t *testing.T, handler http.HandlerFunc) *gemini.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client, err := gemini.New(context.Background(), gemini.Options{
		APIKey: "test-key", DefaultModel: "gemini-test", BaseURL: srv.URL, HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return client
}

func refuseWith429(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = io.WriteString(w, `{"error":{"code":429,"message":"quota","status":"RESOURCE_EXHAUSTED"}}`)
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

// GenerateContentStream is lazy, so the HTTP error only exists once something
// asks for the first item. Asked for inside Chat, it is still Chat's own error.
func TestARefusalBeforeTheFirstItemIsChatsOwnError(t *testing.T) {
	client := newClient(t, refuseWith429)

	_, err := client.Chat(context.Background(), goRequest)

	if !apperr.Is(err, apperr.ErrUnavailable) {
		t.Errorf("err = %v, want ErrUnavailable", err)
	}
	if !ai.Failover(err) {
		t.Errorf("err = %v does not fail over", err)
	}
}

// The behaviour all of this is for: a chain with Gemini in it moves on.
func TestARefusalBeforeTheFirstItemFailsOverToTheNextProvider(t *testing.T) {
	registry := ai.NewRegistry()
	registry.Register(newClient(t, refuseWith429))
	registry.Register(fake.Text("from the second"))
	runner := ai.NewRunner(registry, ai.NewChainSet([]string{"gemini", "fake"}, nil))

	var stream <-chan ai.StreamChunk
	answered, err := runner.Run(context.Background(), ai.RunOptions{}, func(c ai.Client) error {
		opened, err := c.Chat(context.Background(), goRequest)
		stream = opened
		return err
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if answered.Name() != "fake" {
		t.Fatalf("answered by %q, want the second provider", answered.Name())
	}
	for range stream {
	}
}

// The item pulled inside Chat has to reach the reader, in its place.
func TestChatStreamsEveryItemThenUsage(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, frameHel+frameLo)
	})

	ch, err := client.Chat(context.Background(), goRequest)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}

	var text strings.Builder
	var usage *ai.Usage
	var model string
	for _, chunk := range collect(t, ch) {
		if chunk.Err != nil {
			t.Errorf("stream error: %v", chunk.Err)
		}
		text.WriteString(chunk.Text)
		if chunk.Usage != nil {
			usage, model = chunk.Usage, chunk.Model
		}
	}
	if text.String() != "Hello" {
		t.Errorf("text = %q, want Hello", text.String())
	}
	if usage == nil || usage.InputTokens != 7 || usage.OutputTokens != 2 {
		t.Errorf("usage = %+v, want 7/2", usage)
	}
	if model != "gemini-test-001" {
		t.Errorf("model = %q", model)
	}
}

// Once an item has been handed over the reply has started, and a failure goes
// on the channel: a second provider would contradict what is already on screen.
func TestAFailureAfterTheFirstItemArrivesOnTheChannel(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, frameHel+frameErr)
	})

	ch, err := client.Chat(context.Background(), goRequest)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}

	chunks := collect(t, ch)
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want the text and the error: %+v", len(chunks), chunks)
	}
	if chunks[0].Text != "Hel" {
		t.Errorf("first chunk = %+v", chunks[0])
	}
	if !apperr.Is(chunks[1].Err, apperr.ErrUnavailable) {
		t.Errorf("last chunk error = %v, want ErrUnavailable", chunks[1].Err)
	}
}

// panickingBody serves its data and then panics on the next read, which puts a
// panic on the stream's own goroutine: past the first item, Chat has returned and
// only that goroutine is pulling.
type panickingBody struct{ data *strings.Reader }

func (b panickingBody) Read(p []byte) (int, error) {
	if b.data.Len() == 0 {
		panic("boom")
	}
	return b.data.Read(p)
}

func (panickingBody) Close() error { return nil }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A panic on this goroutine has no handler above it, so it would end the process
// and every reply in it. Failing that, the reader must not be left waiting on a
// channel nobody will close.
func TestAPanicInTheStreamClosesTheChannelAndSparesTheProcess(t *testing.T) {
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       panickingBody{data: strings.NewReader(frameHel)},
			Request:    r,
		}, nil
	})

	client, err := gemini.New(context.Background(), gemini.Options{
		APIKey: "test-key", DefaultModel: "gemini-test", BaseURL: "http://stub.invalid",
		HTTPClient: &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	ch, err := client.Chat(context.Background(), goRequest)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	collect(t, ch)
}

// A caller that has gone must not hold the iterator or the connection open.
func TestAnAbandonedStreamCloses(t *testing.T) {
	client := newClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, frameHel+frameLo)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := client.Chat(ctx, goRequest)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	<-ch
	cancel()
	collect(t, ch)
}
