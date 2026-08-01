package openrouter_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/openrouter"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// newClient points a real client at a stub server, so the request encoding and
// SSE parsing are exercised without a key or a network call.
func newClient(t *testing.T, handler http.HandlerFunc) (*openrouter.Client, *[]map[string]any) {
	t.Helper()

	var received []map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var decoded map[string]any
		_ = json.Unmarshal(body, &decoded)
		received = append(received, decoded)
		handler(w, r)
	}))
	t.Cleanup(srv.Close)

	c, err := openrouter.New(openrouter.Options{
		APIKey:     "test-key",
		BaseURL:    srv.URL,
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	return c, &received
}

func TestChatParsesServerSentEvents(t *testing.T) {
	t.Parallel()

	c, _ := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Deliberately includes the shapes a real stream contains: a keep-alive
		// comment, a blank line, an unparseable frame, and the usage frame.
		io.WriteString(w, ": OPENROUTER PROCESSING\n\n")
		io.WriteString(w, `data: {"choices":[{"delta":{"content":"Add "}}]}`+"\n\n")
		io.WriteString(w, `data: {"choices":[{"delta":{"content":"2.5kg."}}]}`+"\n\n")
		io.WriteString(w, "data: {not json}\n\n")
		io.WriteString(w, `data: {"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":4}}`+"\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	})

	ch, err := c.Chat(context.Background(), ai.Request{Messages: []ai.Message{ai.UserText("what next?")}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}

	var text strings.Builder
	var usage *ai.Usage
	for chunk := range ch {
		if chunk.Err != nil {
			t.Fatalf("stream error: %v", chunk.Err)
		}
		if chunk.Usage != nil {
			usage = chunk.Usage
		}
		text.WriteString(chunk.Text)
	}

	if got := text.String(); got != "Add 2.5kg." {
		t.Fatalf("reassembled text = %q", got)
	}
	if usage == nil || usage.InputTokens != 11 || usage.OutputTokens != 4 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestChatRequestsUsageAndMapsRoles(t *testing.T) {
	t.Parallel()

	c, received := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, "data: [DONE]\n\n")
	})

	_, err := c.Chat(context.Background(), ai.Request{
		System: "you are a coach",
		Messages: []ai.Message{
			ai.UserText("hello"),
			ai.ModelText("hi"),
			ai.UserText("what next?"),
		},
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}

	body := (*received)[0]

	if body["stream"] != true {
		t.Error("stream should be true")
	}
	// Without include_usage the final usage frame never arrives and North
	// cannot account for what a conversation cost.
	opts, ok := body["stream_options"].(map[string]any)
	if !ok || opts["include_usage"] != true {
		t.Errorf("stream_options = %v, want include_usage true", body["stream_options"])
	}

	messages, _ := body["messages"].([]any)
	if len(messages) != 4 {
		t.Fatalf("expected the system prompt plus 3 turns, got %d", len(messages))
	}

	want := []string{"system", "user", "assistant", "user"}
	for i, role := range want {
		got := messages[i].(map[string]any)["role"]
		if got != role {
			t.Errorf("message %d role = %v, want %s", i, got, role)
		}
	}
}

func TestGenerateSendsStrictJSONSchema(t *testing.T) {
	t.Parallel()

	c, received := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `{"choices":[{"message":{"content":"{\"name\":\"Push day\"}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":7}}`)
	})

	schema := ai.Object("a plan", map[string]*ai.Schema{
		"name": ai.String("plan name"),
		"days": ai.Integer("training days per week"),
	}, "name", "days")

	resp, err := c.Generate(context.Background(), ai.Request{ResponseSchema: schema})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if resp.Text != `{"name":"Push day"}` {
		t.Fatalf("text = %q", resp.Text)
	}

	format := (*received)[0]["response_format"].(map[string]any)
	if format["type"] != "json_schema" {
		t.Fatalf("response_format type = %v", format["type"])
	}

	jsonSchema := format["json_schema"].(map[string]any)
	if jsonSchema["strict"] != true {
		t.Error("strict mode should be requested")
	}

	// Strict mode rejects a schema that omits additionalProperties:false, so
	// getting this wrong fails every structured call at the provider.
	body := jsonSchema["schema"].(map[string]any)
	if body["additionalProperties"] != false {
		t.Errorf("additionalProperties = %v, want false", body["additionalProperties"])
	}
	if len(body["required"].([]any)) != 2 {
		t.Errorf("required = %v, want both properties", body["required"])
	}
}

func TestErrorStatusesMapToSentinels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status int
		want   error
	}{
		{http.StatusTooManyRequests, apperr.ErrUnavailable},
		{http.StatusInternalServerError, apperr.ErrUnavailable},
		{http.StatusUnauthorized, apperr.ErrForbidden},
		{http.StatusForbidden, apperr.ErrForbidden},
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			t.Parallel()

			c, _ := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				io.WriteString(w, `{"error":"nope"}`)
			})

			_, err := c.Generate(context.Background(), ai.Request{})
			if !apperr.Is(err, tt.want) {
				t.Fatalf("status %d produced %v, want %v", tt.status, err, tt.want)
			}
		})
	}
}

func TestNewRequiresAnAPIKey(t *testing.T) {
	t.Parallel()

	if _, err := openrouter.New(openrouter.Options{}); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

// OpenRouter has no upload endpoint. The error has to be explicit so a caller
// routes video work to Gemini rather than silently sending nothing.
func TestUploadIsUnsupported(t *testing.T) {
	t.Parallel()

	c, _ := newClient(t, func(w http.ResponseWriter, _ *http.Request) {})

	_, err := c.UploadFile(context.Background(), ai.UploadRequest{
		Name: "squat.mp4", MIMEType: "video/mp4", Reader: strings.NewReader("x"),
	})
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}
