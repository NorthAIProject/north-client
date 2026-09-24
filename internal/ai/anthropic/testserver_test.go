package anthropic_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai/anthropic"
)

// fakeAPI stands in for api.anthropic.com. It records every request body and
// answers each with the next canned response.
type fakeAPI struct {
	mu        sync.Mutex
	bodies    []map[string]any
	headers   []http.Header
	responses []cannedResponse
}

type cannedResponse struct {
	status int
	// body is written as-is. For a stream it is the full SSE text.
	body   string
	stream bool
}

func newFakeAPI(t *testing.T, responses ...cannedResponse) (*fakeAPI, *anthropic.Client) {
	t.Helper()
	f := &fakeAPI{responses: responses}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)

		f.mu.Lock()
		f.bodies = append(f.bodies, body)
		f.headers = append(f.headers, r.Header.Clone())
		var res cannedResponse
		if len(f.responses) > 0 {
			res, f.responses = f.responses[0], f.responses[1:]
		}
		f.mu.Unlock()

		if res.stream {
			w.Header().Set("Content-Type", "text/event-stream")
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		if res.status == 0 {
			res.status = http.StatusOK
		}
		w.WriteHeader(res.status)
		_, _ = io.WriteString(w, res.body)
	}))
	t.Cleanup(srv.Close)

	client, err := anthropic.New(anthropic.Options{
		APIKey:       "sk-ant-test",
		DefaultModel: "claude-opus-5",
		BaseURL:      srv.URL,
		HTTPClient:   srv.Client(),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	return f, client
}

func (f *fakeAPI) body(t *testing.T, i int) map[string]any {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if i >= len(f.bodies) {
		t.Fatalf("request %d was never made (%d made)", i, len(f.bodies))
	}
	return f.bodies[i]
}

// textMessage is a complete non-streaming response carrying one text block.
func textMessage(text string) cannedResponse {
	b, _ := json.Marshal(map[string]any{
		"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-5",
		"content":     []any{map[string]any{"type": "text", "text": text}},
		"stop_reason": "end_turn",
		"usage":       map[string]any{"input_tokens": 12, "output_tokens": 5, "cache_read_input_tokens": 100},
	})
	return cannedResponse{body: string(b)}
}

func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }
