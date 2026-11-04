package openaicompat_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/openaicompat"
)

// panickingBody serves its data and then panics on the next read, which puts a
// panic on the stream's own goroutine.
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
		frames := `data: {"choices":[{"delta":{"content":"Hel"}}]}` + "\n\n"
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       panickingBody{data: strings.NewReader(frames)},
			Request:    r,
		}, nil
	})

	c, err := openaicompat.New(openaicompat.Options{
		Name: "testprovider", APIKey: "test-key", BaseURL: "http://stub.invalid", DefaultModel: "m",
		HTTPClient: &http.Client{Transport: transport},
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	ch, err := c.Chat(context.Background(), ai.Request{Messages: []ai.Message{ai.UserText("go")}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	drainWithin(t, ch)
}
