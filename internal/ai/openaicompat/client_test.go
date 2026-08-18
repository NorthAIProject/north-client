package openaicompat_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/openaicompat"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// newClient points a real client at a stub server, so the request encoding and
// SSE parsing are exercised without a key or a network call.
func newClient(t *testing.T, handler http.HandlerFunc) (*openaicompat.Client, *[]map[string]any) {
	t.Helper()
	return newClientWith(t, func(o *openaicompat.Options) { o.SupportsJSONSchema = true }, handler)
}

// newClientWith is newClient with a hook to vary the options under test.
func newClientWith(t *testing.T, configure func(*openaicompat.Options), handler http.HandlerFunc) (*openaicompat.Client, *[]map[string]any) {
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

	opts := openaicompat.Options{
		Name:         "testprovider",
		APIKey:       "test-key",
		BaseURL:      srv.URL,
		DefaultModel: "test-model",
		HTTPClient:   srv.Client(),
	}
	if configure != nil {
		configure(&opts)
	}

	c, err := openaicompat.New(opts)
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
		_, _ = io.WriteString(w, ": OPENROUTER PROCESSING\n\n")
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"Add "}}]}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"2.5kg."}}]}`+"\n\n")
		_, _ = io.WriteString(w, "data: {not json}\n\n")
		_, _ = io.WriteString(w, `data: {"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":4}}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
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
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
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

// The configured name is what the registry keys on and what gets persisted
// against every message, so it has to survive from Options to Name().
func TestNameAndHeadersComeFromOptions(t *testing.T) {
	t.Parallel()

	var gotHeaders http.Header

	c, _ := newClientWith(t, func(o *openaicompat.Options) {
		o.Name = "openrouter"
		o.Headers = map[string]string{
			"HTTP-Referer": "https://north.example",
			"X-Title":      "North",
			"X-Empty":      "",
		}
	}, func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	})

	if c.Name() != "openrouter" {
		t.Fatalf("Name() = %q, want openrouter", c.Name())
	}

	if _, err := c.Generate(context.Background(), ai.Request{}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	if got := gotHeaders.Get("Authorization"); got != "Bearer test-key" {
		t.Errorf("Authorization = %q", got)
	}
	if got := gotHeaders.Get("HTTP-Referer"); got != "https://north.example" {
		t.Errorf("HTTP-Referer = %q", got)
	}
	if got := gotHeaders.Get("X-Title"); got != "North" {
		t.Errorf("X-Title = %q", got)
	}
	// An empty configured value should not become an empty header.
	if _, ok := gotHeaders["X-Empty"]; ok {
		t.Error("blank header values should be dropped, not sent empty")
	}
}

func TestDefaultModelAppliesWhenRequestOmitsOne(t *testing.T) {
	t.Parallel()

	c, received := newClientWith(t, func(o *openaicompat.Options) {
		o.DefaultModel = "meta/llama-3.3-70b-instruct"
	}, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	})

	if _, err := c.Generate(context.Background(), ai.Request{}); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if got := (*received)[0]["model"]; got != "meta/llama-3.3-70b-instruct" {
		t.Errorf("model = %v, want the configured default", got)
	}

	if _, err := c.Generate(context.Background(), ai.Request{Model: "x-ai/grok-4.5"}); err != nil {
		t.Fatalf("generate: %v", err)
	}
	if got := (*received)[1]["model"]; got != "x-ai/grok-4.5" {
		t.Errorf("model = %v, want the per-request override", got)
	}
}

func TestGenerateSendsStrictJSONSchema(t *testing.T) {
	t.Parallel()

	c, received := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"{\"name\":\"Push day\"}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":7}}`)
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

// Providers that reject strict mode must still be asked for the shape, or
// structured callers get prose back and fail to decode every time.
func TestSchemaMovesIntoThePromptWhenStrictModeIsUnsupported(t *testing.T) {
	t.Parallel()

	c, received := newClientWith(t, func(o *openaicompat.Options) {
		o.SupportsJSONSchema = false
	}, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"{}"}}]}`)
	})

	schema := ai.Object("a plan", map[string]*ai.Schema{
		"name": ai.String("plan name"),
	}, "name")

	if _, err := c.Generate(context.Background(), ai.Request{
		System:         "you are a coach",
		ResponseSchema: schema,
	}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	body := (*received)[0]

	// A request the provider would reject outright is worse than one that
	// merely needs re-parsing, so response_format is omitted entirely.
	if _, ok := body["response_format"]; ok {
		t.Errorf("response_format = %v, want it omitted", body["response_format"])
	}

	messages := body["messages"].([]any)
	system := messages[0].(map[string]any)
	if system["role"] != "system" {
		t.Fatalf("first message role = %v", system["role"])
	}

	content, _ := system["content"].(string)
	if !strings.Contains(content, "you are a coach") {
		t.Error("the caller's system prompt should survive")
	}
	if !strings.Contains(content, `"plan name"`) {
		t.Errorf("the schema should be described in the prompt, got %q", content)
	}
}

// A schema instruction with no system prompt must not produce a leading blank.
func TestSchemaInstructionStandsAloneWithoutASystemPrompt(t *testing.T) {
	t.Parallel()

	c, received := newClientWith(t, func(o *openaicompat.Options) {
		o.SupportsJSONSchema = false
	}, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"{}"}}]}`)
	})

	if _, err := c.Generate(context.Background(), ai.Request{
		ResponseSchema: ai.Object("a plan", map[string]*ai.Schema{"name": ai.String("plan name")}, "name"),
	}); err != nil {
		t.Fatalf("generate: %v", err)
	}

	messages := (*received)[0]["messages"].([]any)
	content := messages[0].(map[string]any)["content"].(string)
	if strings.HasPrefix(content, "\n") {
		t.Errorf("content should not start with a blank line, got %q", content)
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
		// Billing has to be its own sentinel: a chain that treats an exhausted
		// balance as a transient overload would keep asking the same dead
		// provider.
		{http.StatusPaymentRequired, apperr.ErrPaymentRequired},
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			t.Parallel()

			c, _ := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, `{"error":"nope"}`)
			})

			_, err := c.Generate(context.Background(), ai.Request{})
			if !apperr.Is(err, tt.want) {
				t.Fatalf("status %d produced %v, want %v", tt.status, err, tt.want)
			}
		})
	}
}

// 402 must not also satisfy ErrUnavailable, or a failover rule keyed on
// "temporarily unavailable" would retry a provider that is out of credit.
func TestPaymentRequiredIsDistinctFromUnavailable(t *testing.T) {
	t.Parallel()

	c, _ := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
		_, _ = io.WriteString(w, `{"error":"insufficient credits"}`)
	})

	_, err := c.Generate(context.Background(), ai.Request{})
	if apperr.Is(err, apperr.ErrUnavailable) {
		t.Fatalf("402 should not match ErrUnavailable, got %v", err)
	}
}

// The error must name the provider that failed, or a chain of four produces
// four indistinguishable log lines.
func TestErrorsNameTheProvider(t *testing.T) {
	t.Parallel()

	c, _ := newClientWith(t, func(o *openaicompat.Options) {
		o.Name = "nvidia"
	}, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	_, err := c.Generate(context.Background(), ai.Request{})
	if err == nil || !strings.Contains(err.Error(), "nvidia") {
		t.Fatalf("error should name the provider, got %v", err)
	}
}

func TestNewRequiresItsConfiguration(t *testing.T) {
	t.Parallel()

	base := openaicompat.Options{
		Name:         "n",
		BaseURL:      "https://example.test/v1",
		APIKey:       "k",
		DefaultModel: "m",
	}

	tests := map[string]func(*openaicompat.Options){
		"name":    func(o *openaicompat.Options) { o.Name = "" },
		"baseURL": func(o *openaicompat.Options) { o.BaseURL = "" },
		"apiKey":  func(o *openaicompat.Options) { o.APIKey = "" },
		"model":   func(o *openaicompat.Options) { o.DefaultModel = "" },
	}

	for missing, blank := range tests {
		t.Run("missing "+missing, func(t *testing.T) {
			t.Parallel()

			opts := base
			blank(&opts)

			if _, err := openaicompat.New(opts); !apperr.Is(err, apperr.ErrValidation) {
				t.Fatalf("expected ErrValidation, got %v", err)
			}
		})
	}

	if _, err := openaicompat.New(base); err != nil {
		t.Fatalf("a complete configuration should build, got %v", err)
	}
}

// The OpenAI chat dialect has no upload endpoint. The error has to be explicit
// so a caller routes video work to Gemini rather than silently sending nothing.
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

func TestChatSendsInlineImagesAsDataURLs(t *testing.T) {
	t.Parallel()

	c, received := newClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	})

	_, err := c.Generate(context.Background(), ai.Request{
		Messages: []ai.Message{{
			Role: ai.RoleUser,
			Parts: []ai.Part{
				ai.TextPart("how's my squat?"),
				{InlineData: []byte{0xff, 0xd8, 0xff}, MIMEType: "image/jpeg"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	messages := (*received)[0]["messages"].([]any)
	content, ok := messages[0].(map[string]any)["content"].([]any)
	if !ok {
		t.Fatalf("content = %T, want a parts array", messages[0].(map[string]any)["content"])
	}
	if len(content) != 2 {
		t.Fatalf("parts = %d, want 2", len(content))
	}
	text := content[0].(map[string]any)
	if text["type"] != "text" || text["text"] != "how's my squat?" {
		t.Fatalf("text part = %+v", text)
	}
	image := content[1].(map[string]any)
	if image["type"] != "image_url" {
		t.Fatalf("image type = %v", image["type"])
	}
	url := image["image_url"].(map[string]any)["url"].(string)
	if !strings.HasPrefix(url, "data:image/jpeg;base64,") {
		t.Fatalf("data url = %q", url)
	}
}
