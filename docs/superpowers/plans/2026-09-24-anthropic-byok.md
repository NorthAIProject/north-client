# Anthropic BYOK Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A person can paste an Anthropic API key in Settings and have Claude answer their coach turns with every Khepri capability (tools, approvals, exercise animation, check-ins) working natively.

**Architecture:** A new `internal/ai/anthropic` package implements `ai.Client` over the native Messages API using the official Go SDK. It is reachable only as a bring-your-own-key provider: a catalogue entry in `internal/ai/providers/catalog.go`, built by `providers.User`, verified by `aicreds`. Thinking blocks produced during a tool round ride back on a new opaque `ai.Message.ProviderState` field so a turn's in-memory tool loop can replay them.

**Tech Stack:** Go 1.x, `github.com/anthropics/anthropic-sdk-go`, `net/http/httptest` for tests, Postgres (`TEST_DATABASE_URL`) only for existing coach tests.

**Spec:** `docs/superpowers/specs/2026-09-24-anthropic-byok-design.md`

## Global Constraints

- Default model: `claude-opus-5`. No date suffixes on model IDs.
- Never send `temperature`, `top_p` or `top_k` (Opus 5 returns 400).
- `MaxTokens` defaults to 16000 when the request leaves it zero.
- Never log or include the API key in any error string.
- Error classes: 401/403 → `apperr.ErrForbidden`; 402 or a credit/billing error → `apperr.ErrPaymentRequired`; 429, 5xx, 529 and `refusal` → `apperr.ErrUnavailable`; any other 4xx → plain error (the runner does not fail over).
- The client does **not** implement `ai.ToolCaller` (it calls tools natively).
- SDK type names come from the SDK and the compiler. Where a plan snippet names an SDK symbol the compiler rejects, fix the name from the compiler error or `go doc github.com/anthropics/anthropic-sdk-go <Symbol>`; do not change behaviour.
- Match surrounding comment style: comments explain why, in full sentences.

## Review Focus

- **History that starts with a model turn** (proactive briefing threads begin with Khepri speaking): Anthropic requires the first message to be `user`, so a leading assistant message must be preceded by a short user placeholder. Test in Task 2.
- **Empty text parts** (stored tool-call rows have no text): Anthropic rejects empty text blocks, so empty parts are skipped and a message left with no blocks is dropped. Test in Task 2.
- **Attachments that are not images** (voice notes, PDFs referenced as parts): only `image/*` becomes an image block; anything else becomes a text note naming the MIME type, never a 400. Test in Task 5.
- **The caller going away mid-stream**: the stream goroutine must stop and close the channel when `ctx` is cancelled, and never block on a send. Test in Task 3.
- **A replayed tool-call turn with no stored thinking** (resumed after an approval): the request must disable thinking rather than 400. Test in Task 6.

---

### Task 1: Client skeleton, non-streaming text

**Files:**
- Create: `internal/ai/anthropic/client.go`
- Create: `internal/ai/anthropic/client_test.go`
- Create: `internal/ai/anthropic/testserver_test.go`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Produces:
  - `type Options struct { APIKey, DefaultModel, BaseURL string; HTTPClient *http.Client }`
  - `func New(opts Options) (*Client, error)` — error when `APIKey` is empty.
  - `func (c *Client) Name() string` → `"anthropic"`
  - `func (c *Client) Generate(ctx context.Context, req ai.Request) (*ai.Response, error)`
  - `func (c *Client) UploadFile(ctx context.Context, req ai.UploadRequest) (*ai.File, error)` → `&ai.File{}` (empty URI)
  - `func (c *Client) Chat(...)` stub returning an error (filled in Task 3)
  - Unexported `func (c *Client) params(req ai.Request) anthropic.MessageNewParams` used by Tasks 2–6.

- [ ] **Step 1: Add the dependency**

Run: `go get github.com/anthropics/anthropic-sdk-go@latest`
Expected: `go.mod` gains the module.

- [ ] **Step 2: Write the test server helper**

`internal/ai/anthropic/testserver_test.go`:

```go
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
```

- [ ] **Step 3: Write the failing tests**

`internal/ai/anthropic/client_test.go`:

```go
package anthropic_test

import (
	"context"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/anthropic"
)

func TestNewRefusesAnEmptyKey(t *testing.T) {
	if _, err := anthropic.New(anthropic.Options{}); err == nil {
		t.Fatal("a client with no key was built")
	}
}

func TestGenerateSendsTheSystemPromptCachedAndReadsTheReply(t *testing.T) {
	api, client := newFakeAPI(t, textMessage("Sit back and down."))

	resp, err := client.Generate(context.Background(), ai.Request{
		System:   "You are Khepri.",
		Messages: []ai.Message{ai.UserText("show me a squat")},
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	if resp.Text != "Sit back and down." {
		t.Errorf("text = %q", resp.Text)
	}
	if resp.Model != "claude-opus-5" {
		t.Errorf("model = %q", resp.Model)
	}
	// Cache reads are input the account paid for; the ledger must see them.
	if resp.Usage.InputTokens != 112 || resp.Usage.OutputTokens != 5 {
		t.Errorf("usage = %+v, want 112 in (12 + 100 cached), 5 out", resp.Usage)
	}

	body := api.body(t, 0)
	if body["model"] != "claude-opus-5" {
		t.Errorf("model sent = %v", body["model"])
	}
	if body["max_tokens"] != float64(16000) {
		t.Errorf("max_tokens = %v, want the 16000 default", body["max_tokens"])
	}
	for _, key := range []string{"temperature", "top_p", "top_k"} {
		if _, ok := body[key]; ok {
			t.Errorf("%s was sent; Opus 5 rejects sampling parameters", key)
		}
	}
	system, _ := body["system"].([]any)
	if len(system) != 1 {
		t.Fatalf("system = %v, want one block", body["system"])
	}
	block := system[0].(map[string]any)
	if block["text"] != "You are Khepri." || block["cache_control"] == nil {
		t.Errorf("system block = %v, want the prompt with cache_control", block)
	}
}

func TestUploadFileAsksCallersToInlineTheBytes(t *testing.T) {
	_, client := newFakeAPI(t)
	f, err := client.UploadFile(context.Background(), ai.UploadRequest{})
	if err != nil || f == nil || f.URI != "" {
		t.Fatalf("upload = %+v, %v; want an empty URI and no error", f, err)
	}
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `go test ./internal/ai/anthropic/`
Expected: FAIL — package `anthropic` has no `New`.

- [ ] **Step 5: Write the implementation**

`internal/ai/anthropic/client.go`:

```go
// Package anthropic is Khepri's client for Claude, over Anthropic's native
// Messages API.
//
// Reached only through a key a person brings themselves (see
// providers.Catalog). It calls Khepri's tools natively, so it does not
// implement ai.ToolCaller: the coach runs its own tool loop against it, and
// the MCP bridge built for gateways never applies.
package anthropic

import (
	"context"
	"errors"
	"net/http"
	"strings"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/NorthAIProject/north-client/internal/ai"
)

// defaultMaxTokens is used when a request names no limit. High enough that a
// considered answer is not cut off, low enough to stay under the SDK's
// non-streaming timeout.
const defaultMaxTokens = 16000

type Options struct {
	APIKey       string
	DefaultModel string

	// BaseURL overrides api.anthropic.com. Tests only.
	BaseURL string

	// HTTPClient is shared across users so a per-user client reuses
	// connections instead of paying a TLS handshake per turn.
	HTTPClient *http.Client
}

type Client struct {
	sdk          sdk.Client
	defaultModel string
}

func New(opts Options) (*Client, error) {
	if strings.TrimSpace(opts.APIKey) == "" {
		return nil, errors.New("anthropic: an API key is required")
	}

	reqOpts := []option.RequestOption{option.WithAPIKey(opts.APIKey)}
	if opts.BaseURL != "" {
		reqOpts = append(reqOpts, option.WithBaseURL(opts.BaseURL))
	}
	if opts.HTTPClient != nil {
		reqOpts = append(reqOpts, option.WithHTTPClient(opts.HTTPClient))
	}

	model := opts.DefaultModel
	if model == "" {
		model = "claude-opus-5"
	}
	return &Client{sdk: sdk.NewClient(reqOpts...), defaultModel: model}, nil
}

func (c *Client) Name() string { return "anthropic" }

func (c *Client) Generate(ctx context.Context, req ai.Request) (*ai.Response, error) {
	msg, err := c.sdk.Messages.New(ctx, c.params(req))
	if err != nil {
		return nil, classify(err)
	}
	return fromMessage(msg), nil
}

func (c *Client) Chat(ctx context.Context, req ai.Request) (<-chan ai.StreamChunk, error) {
	return nil, errors.New("anthropic: streaming is not implemented yet")
}

// UploadFile returns an empty URI: images go inline as base64 blocks, which
// every caller already does for providers with no upload step.
func (c *Client) UploadFile(context.Context, ai.UploadRequest) (*ai.File, error) {
	return &ai.File{}, nil
}

func (c *Client) params(req ai.Request) sdk.MessageNewParams {
	model := req.Model
	if model == "" {
		model = c.defaultModel
	}
	maxTokens := int64(req.MaxTokens)
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	p := sdk.MessageNewParams{
		Model:     sdk.Model(model),
		MaxTokens: maxTokens,
		Messages:  toMessages(req.Messages),
	}
	if req.System != "" {
		// Cached because Khepri's context block is most of every request, and
		// it is identical between the rounds of one turn.
		p.System = []sdk.TextBlockParam{{
			Text:         req.System,
			CacheControl: sdk.NewCacheControlEphemeralParam(),
		}}
	}
	return p
}

// toMessages is filled in by Task 2; for now, text only.
func toMessages(in []ai.Message) []sdk.MessageParam {
	out := make([]sdk.MessageParam, 0, len(in))
	for _, m := range in {
		var blocks []sdk.ContentBlockParamUnion
		for _, part := range m.Parts {
			if part.Text != "" {
				blocks = append(blocks, sdk.NewTextBlock(part.Text))
			}
		}
		if m.Role == ai.RoleModel {
			out = append(out, sdk.NewAssistantMessage(blocks...))
		} else {
			out = append(out, sdk.NewUserMessage(blocks...))
		}
	}
	return out
}

func fromMessage(msg *sdk.Message) *ai.Response {
	resp := &ai.Response{
		Model:        string(msg.Model),
		FinishReason: string(msg.StopReason),
		Usage:        usage(msg.Usage),
	}
	var text strings.Builder
	for _, block := range msg.Content {
		if b, ok := block.AsAny().(sdk.TextBlock); ok {
			text.WriteString(b.Text)
		}
	}
	resp.Text = text.String()
	return resp
}

// usage counts cached input as input: the account paid for it, and a ledger
// that dropped it would under-report exactly the long-context turns.
func usage(u sdk.Usage) ai.Usage {
	return ai.Usage{
		InputTokens:  int(u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens),
		OutputTokens: int(u.OutputTokens),
	}
}

// classify is filled in by Task 4.
func classify(err error) error { return err }

var _ ai.Client = (*Client)(nil)
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/ai/anthropic/`
Expected: PASS (fix SDK symbol names from compiler errors if any).

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/ai/anthropic
git commit -m "ai/anthropic: client over the Messages API, text replies"
```

---

### Task 2: History mapping — tools, tool calls, tool results, alternation

**Files:**
- Create: `internal/ai/anthropic/messages.go` (move `toMessages` here)
- Modify: `internal/ai/anthropic/client.go` (params: tools; fromMessage: tool calls)
- Create: `internal/ai/anthropic/messages_test.go`

**Interfaces:**
- Consumes: `params`, `fromMessage` from Task 1.
- Produces: `toMessages(in []ai.Message) []sdk.MessageParam` honouring the rules below; `toTools(tools []ai.Tool) []sdk.ToolUnionParam`; `fromMessage` fills `ToolCalls`.

Rules `toMessages` implements:
1. Empty text parts are skipped; a message with no blocks after that is dropped.
2. `ToolCalls` become `tool_use` blocks (id, name, input from `Arguments`) on an assistant message, after any text.
3. `ToolResults` become `tool_result` blocks (`tool_use_id`, content, `is_error`) on one user message.
4. Consecutive messages of the same role are merged into one.
5. If the first message is an assistant message, a user message with text `"(continuing our conversation)"` is prepended.

- [ ] **Step 1: Write the failing tests**

`internal/ai/anthropic/messages_test.go`:

```go
package anthropic_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
)

func sent(t *testing.T, api *fakeAPI) []map[string]any {
	t.Helper()
	raw, _ := api.body(t, 0)["messages"].([]any)
	out := make([]map[string]any, len(raw))
	for i, m := range raw {
		out[i] = m.(map[string]any)
	}
	return out
}

func blocks(m map[string]any) []map[string]any {
	raw, _ := m["content"].([]any)
	out := make([]map[string]any, len(raw))
	for i, b := range raw {
		out[i] = b.(map[string]any)
	}
	return out
}

func TestAToolRoundTripIsSentInAnthropicsShape(t *testing.T) {
	api, client := newFakeAPI(t, textMessage("Feet shoulder-width."))

	_, err := client.Generate(context.Background(), ai.Request{
		Messages: []ai.Message{
			ai.UserText("show me a squat"),
			ai.ToolCallMessage([]ai.ToolCall{{ID: "toolu_1", Name: "get_exercise", Arguments: json.RawMessage(`{"slug":"barbell-full-squat"}`)}}),
			ai.ToolResultMessage([]ai.ToolResult{
				{ID: "toolu_1", Name: "get_exercise", Content: "Barbell Full Squat"},
			}),
		},
		Tools: []ai.Tool{{Name: "get_exercise", Description: "Read one exercise.",
			Parameters: ai.Object("which", map[string]*ai.Schema{"slug": ai.String("slug")}, "slug")}},
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	msgs := sent(t, api)
	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want 3", len(msgs))
	}
	use := blocks(msgs[1])[0]
	if msgs[1]["role"] != "assistant" || use["type"] != "tool_use" || use["id"] != "toolu_1" || use["name"] != "get_exercise" {
		t.Errorf("tool call sent as %v", msgs[1])
	}
	if input, _ := use["input"].(map[string]any); input["slug"] != "barbell-full-squat" {
		t.Errorf("tool input = %v", use["input"])
	}
	result := blocks(msgs[2])[0]
	if msgs[2]["role"] != "user" || result["type"] != "tool_result" || result["tool_use_id"] != "toolu_1" {
		t.Errorf("tool result sent as %v", msgs[2])
	}

	tools, _ := api.body(t, 0)["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %v", api.body(t, 0)["tools"])
	}
	tool := tools[0].(map[string]any)
	schema, _ := tool["input_schema"].(map[string]any)
	if tool["name"] != "get_exercise" || schema["type"] != "object" {
		t.Errorf("tool = %v", tool)
	}
}

func TestAllResultsOfOneRoundGoInOneUserMessage(t *testing.T) {
	api, client := newFakeAPI(t, textMessage("ok"))
	_, err := client.Generate(context.Background(), ai.Request{Messages: []ai.Message{
		ai.UserText("two things"),
		ai.ToolCallMessage([]ai.ToolCall{
			{ID: "a", Name: "x", Arguments: json.RawMessage(`{}`)},
			{ID: "b", Name: "y", Arguments: json.RawMessage(`{}`)},
		}),
		ai.ToolResultMessage([]ai.ToolResult{{ID: "a", Content: "1"}, {ID: "b", Content: "no", IsError: true}}),
	}})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	results := blocks(sent(t, api)[2])
	if len(results) != 2 || results[1]["is_error"] != true {
		t.Errorf("results = %v, want both, the second marked as an error", results)
	}
}

func TestSameRoleMessagesAreMergedAndEmptyTextIsDropped(t *testing.T) {
	api, client := newFakeAPI(t, textMessage("ok"))
	_, err := client.Generate(context.Background(), ai.Request{Messages: []ai.Message{
		ai.UserText("first"),
		ai.UserText("second"),
		{Role: ai.RoleModel, Parts: []ai.Part{ai.TextPart("")}},
		ai.UserText("third"),
	}})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	msgs := sent(t, api)
	if len(msgs) != 1 || len(blocks(msgs[0])) != 3 {
		t.Errorf("messages = %v, want one user message with three text blocks", msgs)
	}
}

func TestAThreadThatBeginsWithKhepriStillStartsWithAUserTurn(t *testing.T) {
	api, client := newFakeAPI(t, textMessage("ok"))
	_, err := client.Generate(context.Background(), ai.Request{Messages: []ai.Message{
		{Role: ai.RoleModel, Parts: []ai.Part{ai.TextPart("Good morning — today is Upper B.")}},
		ai.UserText("thanks"),
	}})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	msgs := sent(t, api)
	if len(msgs) != 3 || msgs[0]["role"] != "user" || msgs[1]["role"] != "assistant" {
		t.Errorf("messages = %v, want a user placeholder before the briefing", msgs)
	}
}

func TestAToolCallInTheReplyIsReturned(t *testing.T) {
	b, _ := json.Marshal(map[string]any{
		"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-5",
		"content": []any{map[string]any{"type": "tool_use", "id": "toolu_9", "name": "get_exercise",
			"input": map[string]any{"slug": "squat"}}},
		"stop_reason": "tool_use",
		"usage":       map[string]any{"input_tokens": 1, "output_tokens": 1},
	})
	_, client := newFakeAPI(t, cannedResponse{body: string(b)})

	resp, err := client.Generate(context.Background(), ai.Request{Messages: []ai.Message{ai.UserText("squat?")}})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ID != "toolu_9" || resp.ToolCalls[0].Name != "get_exercise" {
		t.Fatalf("tool calls = %+v", resp.ToolCalls)
	}
	var args map[string]string
	if err := json.Unmarshal(resp.ToolCalls[0].Arguments, &args); err != nil || args["slug"] != "squat" {
		t.Errorf("arguments = %s", resp.ToolCalls[0].Arguments)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ai/anthropic/ -run 'Tool|Merged|Begins'`
Expected: FAIL — tool blocks missing, messages not merged.

- [ ] **Step 3: Write the implementation**

Create `internal/ai/anthropic/messages.go` (and delete `toMessages` from `client.go`):

```go
package anthropic

import (
	"encoding/json"

	sdk "github.com/anthropics/anthropic-sdk-go"

	"github.com/NorthAIProject/north-client/internal/ai"
)

// openingPlaceholder precedes a history that begins with Khepri speaking — a
// proactive briefing thread — because the API requires the first turn to be
// the user's.
const openingPlaceholder = "(continuing our conversation)"

// toMessages turns Khepri's history into the alternating user/assistant turns
// the Messages API requires.
func toMessages(in []ai.Message) []sdk.MessageParam {
	type turn struct {
		assistant bool
		blocks    []sdk.ContentBlockParamUnion
	}
	var turns []turn

	for _, m := range in {
		assistant := m.Role == ai.RoleModel
		blocks := contentBlocks(m)
		if len(blocks) == 0 {
			// A stored row with nothing a model can read, such as an empty
			// text part. Sending it would be a 400.
			continue
		}
		if n := len(turns); n > 0 && turns[n-1].assistant == assistant {
			turns[n-1].blocks = append(turns[n-1].blocks, blocks...)
			continue
		}
		turns = append(turns, turn{assistant: assistant, blocks: blocks})
	}

	if len(turns) > 0 && turns[0].assistant {
		turns = append([]turn{{blocks: []sdk.ContentBlockParamUnion{sdk.NewTextBlock(openingPlaceholder)}}}, turns...)
	}

	out := make([]sdk.MessageParam, 0, len(turns))
	for _, t := range turns {
		if t.assistant {
			out = append(out, sdk.NewAssistantMessage(t.blocks...))
		} else {
			out = append(out, sdk.NewUserMessage(t.blocks...))
		}
	}
	return out
}

func contentBlocks(m ai.Message) []sdk.ContentBlockParamUnion {
	var blocks []sdk.ContentBlockParamUnion
	for _, part := range m.Parts {
		if part.Text != "" {
			blocks = append(blocks, sdk.NewTextBlock(part.Text))
		}
	}
	for _, call := range m.ToolCalls {
		var input any = map[string]any{}
		if len(call.Arguments) > 0 {
			_ = json.Unmarshal(call.Arguments, &input)
		}
		blocks = append(blocks, sdk.NewToolUseBlock(call.ID, input, call.Name))
	}
	for _, result := range m.ToolResults {
		blocks = append(blocks, sdk.NewToolResultBlock(result.ID, result.Content, result.IsError))
	}
	return blocks
}

func toTools(tools []ai.Tool) []sdk.ToolUnionParam {
	out := make([]sdk.ToolUnionParam, 0, len(tools))
	for _, t := range tools {
		schema := ai.JSONSchema(t.Parameters)
		param := sdk.ToolParam{
			Name:        t.Name,
			Description: sdk.String(t.Description),
			InputSchema: sdk.ToolInputSchemaParam{Properties: schema["properties"]},
		}
		if required, ok := schema["required"].([]string); ok {
			param.InputSchema.Required = required
		}
		out = append(out, sdk.ToolUnionParam{OfTool: &param})
	}
	return out
}
```

In `client.go` `params`, after `Messages`:

```go
	if len(req.Tools) > 0 {
		p.Tools = toTools(req.Tools)
	}
```

In `fromMessage`, inside the content loop:

```go
		switch b := block.AsAny().(type) {
		case sdk.TextBlock:
			text.WriteString(b.Text)
		case sdk.ToolUseBlock:
			resp.ToolCalls = append(resp.ToolCalls, ai.ToolCall{
				ID: b.ID, Name: b.Name, Arguments: json.RawMessage(b.JSON.Input.Raw()),
			})
		}
```

(replace the single `if b, ok := ...TextBlock` with this switch; add `encoding/json` to the imports).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ai/anthropic/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ai/anthropic
git commit -m "ai/anthropic: tools, tool calls and results, alternating turns"
```

---

### Task 3: Streaming Chat

**Files:**
- Create: `internal/ai/anthropic/stream.go`
- Modify: `internal/ai/anthropic/client.go` (remove the `Chat` stub)
- Create: `internal/ai/anthropic/stream_test.go`

**Interfaces:**
- Consumes: `params`, `usage`, `classify`.
- Produces: `func (c *Client) Chat(ctx context.Context, req ai.Request) (<-chan ai.StreamChunk, error)` — text chunks in order; then, if the model asked for tools, one chunk with `ToolCalls`; then one chunk with `Usage` and `Model`. An error is always the last chunk. The channel is always closed.

- [ ] **Step 1: Write the failing tests**

`internal/ai/anthropic/stream_test.go`:

```go
package anthropic_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai"
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
```

Add to `testserver_test.go`:

```go
func jsonUnmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ai/anthropic/ -run Chat -run Abandoned`
Expected: FAIL — "streaming is not implemented yet".

- [ ] **Step 3: Write the implementation**

`internal/ai/anthropic/stream.go`:

```go
package anthropic

import (
	"context"
	"encoding/json"

	sdk "github.com/anthropics/anthropic-sdk-go"

	"github.com/NorthAIProject/north-client/internal/ai"
)

// Chat streams a reply: text as it arrives, then any tool calls in one chunk
// (a half-built argument object is of no use to the coach), then usage.
func (c *Client) Chat(ctx context.Context, req ai.Request) (<-chan ai.StreamChunk, error) {
	stream := c.sdk.Messages.NewStreaming(ctx, c.params(req))
	out := make(chan ai.StreamChunk)

	go func() {
		defer close(out)
		defer func() { _ = stream.Close() }()

		var msg sdk.Message
		for stream.Next() {
			event := stream.Current()
			if err := msg.Accumulate(event); err != nil {
				send(ctx, out, ai.StreamChunk{Err: classify(err)})
				return
			}
			if delta, ok := event.AsAny().(sdk.ContentBlockDeltaEvent); ok {
				if text, ok := delta.Delta.AsAny().(sdk.TextDelta); ok && text.Text != "" {
					if !send(ctx, out, ai.StreamChunk{Text: text.Text}) {
						return
					}
				}
			}
		}
		if err := stream.Err(); err != nil {
			send(ctx, out, ai.StreamChunk{Err: classify(err)})
			return
		}
		if err := stopError(msg.StopReason); err != nil {
			send(ctx, out, ai.StreamChunk{Err: err})
			return
		}

		if calls := toolCalls(msg.Content); len(calls) > 0 {
			if !send(ctx, out, ai.StreamChunk{ToolCalls: calls}) {
				return
			}
		}
		u := usage(msg.Usage)
		send(ctx, out, ai.StreamChunk{Usage: &u, Model: string(msg.Model)})
	}()

	return out, nil
}

func toolCalls(content []sdk.ContentBlockUnion) []ai.ToolCall {
	var calls []ai.ToolCall
	for _, block := range content {
		if b, ok := block.AsAny().(sdk.ToolUseBlock); ok {
			calls = append(calls, ai.ToolCall{ID: b.ID, Name: b.Name, Arguments: json.RawMessage(b.JSON.Input.Raw())})
		}
	}
	return calls
}

// send delivers a chunk unless the caller has gone away. False means stop.
func send(ctx context.Context, out chan<- ai.StreamChunk, chunk ai.StreamChunk) bool {
	select {
	case out <- chunk:
		return true
	case <-ctx.Done():
		return false
	}
}

// stopError is filled in by Task 4.
func stopError(sdk.StopReason) error { return nil }
```

Use `toolCalls(msg.Content)` inside `fromMessage` too, replacing the `ToolUseBlock` case added in Task 2, so the conversion lives in one place.

If `msg.Accumulate` leaves `ToolUseBlock` input empty after `input_json_delta` events, build the input per block index from the `InputJSONDelta.PartialJSON` fragments instead and assert the test above still passes.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -race ./internal/ai/anthropic/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ai/anthropic
git commit -m "ai/anthropic: stream replies, hand tool calls back whole"
```

---

### Task 4: Errors and stop reasons

**Files:**
- Create: `internal/ai/anthropic/errors.go` (replace the `classify` and `stopError` stubs)
- Create: `internal/ai/anthropic/errors_test.go`

**Interfaces:**
- Produces: `classify(err error) error`, `stopError(reason sdk.StopReason) error` per Global Constraints.

- [ ] **Step 1: Write the failing tests**

`internal/ai/anthropic/errors_test.go`:

```go
package anthropic_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func apiError(status int, kind, message string) cannedResponse {
	b, _ := json.Marshal(map[string]any{"type": "error", "error": map[string]any{"type": kind, "message": message}})
	return cannedResponse{status: status, body: string(b)}
}

func TestErrorsLandInTheClassesTheRunnerActsOn(t *testing.T) {
	cases := map[string]struct {
		res  cannedResponse
		want error
	}{
		"bad key":        {apiError(401, "authentication_error", "invalid x-api-key"), apperr.ErrForbidden},
		"no credit":      {apiError(400, "invalid_request_error", "Your credit balance is too low to access the Anthropic API."), apperr.ErrPaymentRequired},
		"rate limited":   {apiError(429, "rate_limit_error", "slow down"), apperr.ErrUnavailable},
		"overloaded":     {apiError(529, "overloaded_error", "Overloaded"), apperr.ErrUnavailable},
		"server error":   {apiError(500, "api_error", "boom"), apperr.ErrUnavailable},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// Retries are the SDK's; give it the same answer every time.
			_, client := newFakeAPI(t, tc.res, tc.res, tc.res, tc.res)
			_, err := client.Generate(context.Background(), ai.Request{Messages: []ai.Message{ai.UserText("hi")}})
			if !apperr.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if strings.Contains(err.Error(), "sk-ant-test") {
				t.Error("the error carries the key")
			}
		})
	}
}

func TestAMalformedRequestDoesNotWalkTheChain(t *testing.T) {
	_, client := newFakeAPI(t, apiError(400, "invalid_request_error", "messages: roles must alternate"))
	_, err := client.Generate(context.Background(), ai.Request{Messages: []ai.Message{ai.UserText("hi")}})
	if err == nil || ai.Failover(err) {
		t.Fatalf("err = %v; a caller error must not fail over", err)
	}
}

func TestARefusalFailsOverRatherThanPostingNothing(t *testing.T) {
	b, _ := json.Marshal(map[string]any{
		"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-5",
		"content": []any{}, "stop_reason": "refusal",
		"usage": map[string]any{"input_tokens": 1, "output_tokens": 0},
	})
	_, client := newFakeAPI(t, cannedResponse{body: string(b)})
	_, err := client.Generate(context.Background(), ai.Request{Messages: []ai.Message{ai.UserText("hi")}})
	if !apperr.Is(err, apperr.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable so the chain answers", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ai/anthropic/ -run 'Errors|Malformed|Refusal'`
Expected: FAIL.

- [ ] **Step 3: Write the implementation**

`internal/ai/anthropic/errors.go` (delete the stubs in `client.go` and `stream.go`):

```go
package anthropic

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	sdk "github.com/anthropics/anthropic-sdk-go"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// classify puts an API failure in the class the runner acts on. The SDK's
// message is kept, the request is not: it carries the key in a header.
func classify(err error) error {
	var apiErr *sdk.Error
	if !errors.As(err, &apiErr) {
		return fmt.Errorf("anthropic: %w", err)
	}

	detail := strings.TrimSpace(apiErr.Message)
	if detail == "" {
		detail = http.StatusText(apiErr.StatusCode)
	}
	status := apiErr.StatusCode

	switch {
	// Anthropic reports an empty balance as a 400, not a 402. Waiting will not
	// fix it and it says nothing about the key, so it is billing.
	case status == http.StatusPaymentRequired, strings.Contains(strings.ToLower(detail), "credit balance"):
		return fmt.Errorf("%w: anthropic returned %d: %s", apperr.ErrPaymentRequired, status, detail)
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return fmt.Errorf("%w: anthropic returned %d: %s", apperr.ErrForbidden, status, detail)
	case status == http.StatusTooManyRequests, status >= 500:
		return fmt.Errorf("%w: anthropic returned %d: %s", apperr.ErrUnavailable, status, detail)
	default:
		return fmt.Errorf("anthropic returned %d: %s", status, detail)
	}
}

// stopError turns a stop reason that carries no answer into an error. A
// refusal is a declined request, not a reply: posting it would be an empty
// message, and the next provider in the chain may well answer.
func stopError(reason sdk.StopReason) error {
	if reason == sdk.StopReasonRefusal {
		return fmt.Errorf("%w: anthropic declined the request", apperr.ErrUnavailable)
	}
	return nil
}
```

In `Generate`, after the call succeeds:

```go
	if err := stopError(msg.StopReason); err != nil {
		return nil, err
	}
```

If `apiErr.Message` does not exist on `*sdk.Error`, read the message from the raw response body the SDK exposes on the error (the compiler and `go doc github.com/anthropics/anthropic-sdk-go Error` show which field), never from the request.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ai/anthropic/`
Expected: PASS. The SDK retries 429/5xx twice by default; the four canned responses cover that.

- [ ] **Step 5: Commit**

```bash
git add internal/ai/anthropic
git commit -m "ai/anthropic: map errors and refusals onto the runner's classes"
```

---

### Task 5: Images and structured output

**Files:**
- Modify: `internal/ai/anthropic/messages.go` (`contentBlocks`)
- Modify: `internal/ai/anthropic/client.go` (`params`: `ResponseSchema`)
- Test: `internal/ai/anthropic/messages_test.go`, `internal/ai/anthropic/client_test.go`

**Interfaces:**
- Produces: image parts → base64 image blocks; non-image inline parts → text note `"[attachment: <mime> not shown]"`; `ResponseSchema` → `output_config.format` JSON schema.

- [ ] **Step 1: Write the failing tests**

Append to `messages_test.go`:

```go
func TestAPhotoIsSentInlineAndOtherAttachmentsAreNamed(t *testing.T) {
	api, client := newFakeAPI(t, textMessage("Nice depth."))
	_, err := client.Generate(context.Background(), ai.Request{Messages: []ai.Message{{
		Role: ai.RoleUser,
		Parts: []ai.Part{
			{InlineData: []byte{0xff, 0xd8, 0xff}, MIMEType: "image/jpeg"},
			{InlineData: []byte("OggS"), MIMEType: "audio/ogg"},
			ai.TextPart("how is my squat?"),
		},
	}}})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	got := blocks(sent(t, api)[0])
	if len(got) != 3 {
		t.Fatalf("blocks = %v", got)
	}
	source, _ := got[0]["source"].(map[string]any)
	if got[0]["type"] != "image" || source["type"] != "base64" || source["media_type"] != "image/jpeg" || source["data"] != "/9j/" {
		t.Errorf("image block = %v", got[0])
	}
	if got[1]["type"] != "text" || !strings.Contains(got[1]["text"].(string), "audio/ogg") {
		t.Errorf("voice note sent as %v, want a text note naming it", got[1])
	}
}
```

(add `"strings"` to that file's imports).

Append to `client_test.go`:

```go
func TestAResponseSchemaAsksForJSON(t *testing.T) {
	api, client := newFakeAPI(t, textMessage(`{"days":3}`))
	_, err := client.Generate(context.Background(), ai.Request{
		Messages:       []ai.Message{ai.UserText("plan")},
		ResponseSchema: ai.Object("plan", map[string]*ai.Schema{"days": ai.Integer("days")}, "days"),
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	cfg, _ := api.body(t, 0)["output_config"].(map[string]any)
	format, _ := cfg["format"].(map[string]any)
	if format["type"] != "json_schema" || format["schema"] == nil {
		t.Errorf("output_config = %v, want a json_schema format", api.body(t, 0)["output_config"])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ai/anthropic/ -run 'Photo|ResponseSchema'`
Expected: FAIL.

- [ ] **Step 3: Write the implementation**

In `contentBlocks`, replace the parts loop:

```go
	for _, part := range m.Parts {
		switch {
		case part.Text != "":
			blocks = append(blocks, sdk.NewTextBlock(part.Text))
		case len(part.InlineData) > 0 && strings.HasPrefix(part.MIMEType, "image/"):
			blocks = append(blocks, sdk.NewImageBlockBase64(part.MIMEType,
				base64.StdEncoding.EncodeToString(part.InlineData)))
		case len(part.InlineData) > 0 || part.FileURI != "":
			// Claude cannot take this kind of file inline. Naming it keeps
			// the turn honest without failing the whole request.
			blocks = append(blocks, sdk.NewTextBlock("[attachment: "+part.MIMEType+" not shown]"))
		}
	}
```

(imports: `encoding/base64`, `strings`).

In `params`:

```go
	if req.ResponseSchema != nil {
		p.OutputConfig = sdk.OutputConfigParam{
			Format: sdk.JSONOutputFormatParam{Schema: ai.JSONSchema(req.ResponseSchema)},
		}
	}
```

If `OutputConfigParam` / `JSONOutputFormatParam` are named differently in the installed SDK, use `go doc github.com/anthropics/anthropic-sdk-go MessageNewParams` to find the output-config field; if the SDK version has none, set it through `option.WithJSONSet("output_config", map[string]any{"format": map[string]any{"type": "json_schema", "schema": ai.JSONSchema(req.ResponseSchema)}})` on the request instead.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ai/anthropic/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/ai/anthropic
git commit -m "ai/anthropic: inline photos, structured output"
```

---

### Task 6: Thinking across tool rounds

**Files:**
- Modify: `internal/ai/client.go` (`Message.ProviderState`, `StreamChunk.ProviderState`)
- Modify: `internal/ai/tools.go` (no change to `ToolCallMessage`; document the field)
- Modify: `internal/coach/service.go` (pump: carry `ProviderState` onto the tool-call message)
- Modify: `internal/ai/fake/fake.go` (`Response.ProviderState`, emitted with tool calls)
- Modify: `internal/ai/anthropic/stream.go`, `messages.go`, `client.go`
- Create: `internal/ai/anthropic/thinking_test.go`
- Test: `internal/coach/tools_test.go` (one new test)

**Interfaces:**
- Produces:
  - `ai.Message.ProviderState json.RawMessage` — opaque, owned by the client that produced it; other clients ignore it.
  - `ai.StreamChunk.ProviderState json.RawMessage` — set on the chunk carrying `ToolCalls`.
  - The coach sets `ProviderState` on the `ai.ToolCallMessage` it appends after a round.

- [ ] **Step 1: Confirm the replay rule**

Read the tool-use and extended-thinking pages listed in the claude-api skill's `shared/live-sources.md` (WebFetch). Confirm: when a turn continues after tool results with thinking enabled, must the assistant message holding `tool_use` include that response's `thinking` blocks unchanged? Record the answer in the commit message. **If the blocks are optional**, skip Steps 2–7 of this task, delete the "Thinking across tool rounds" section from the spec, and commit only that doc change.

- [ ] **Step 2: Write the failing tests**

`internal/ai/anthropic/thinking_test.go`:

```go
package anthropic_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
)

func TestThinkingFromAToolRoundIsSentBackWithTheCall(t *testing.T) {
	api, client := newFakeAPI(t,
		sse(evStart,
			`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"look it up"}}`,
			`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"sig-1"}}`,
			evStop0,
			`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"get_exercise","input":{}}}`,
			`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{}"}}`,
			`{"type":"content_block_stop","index":1}`,
			evMsgDelta("tool_use", 5), evMsgStop),
		textMessage("done"),
	)

	ch, err := client.Chat(context.Background(), ai.Request{Messages: []ai.Message{ai.UserText("squat?")}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	var calls []ai.ToolCall
	var state json.RawMessage
	for _, c := range collect(t, ch) {
		if len(c.ToolCalls) > 0 {
			calls, state = c.ToolCalls, c.ProviderState
		}
	}
	if len(state) == 0 {
		t.Fatal("the tool-call chunk carried no thinking state")
	}

	callMsg := ai.ToolCallMessage(calls)
	callMsg.ProviderState = state
	_, err = client.Generate(context.Background(), ai.Request{Messages: []ai.Message{
		ai.UserText("squat?"), callMsg,
		ai.ToolResultMessage([]ai.ToolResult{{ID: "toolu_1", Content: "Barbell Full Squat"}}),
	}})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	msgs := sentAt(t, api, 1)
	first := blocks(msgs[1])[0]
	if first["type"] != "thinking" || first["signature"] != "sig-1" || first["thinking"] != "look it up" {
		t.Errorf("assistant turn opens with %v, want the thinking block unchanged", first)
	}
	if _, ok := api.body(t, 1)["thinking"]; ok {
		t.Error("thinking was configured on a request that has its state")
	}
}

func TestAReplayedCallWithNoStateTurnsThinkingOff(t *testing.T) {
	api, client := newFakeAPI(t, textMessage("done"))
	_, err := client.Generate(context.Background(), ai.Request{Messages: []ai.Message{
		ai.UserText("log my check-in"),
		ai.ToolCallMessage([]ai.ToolCall{{ID: "toolu_2", Name: "create_check_in", Arguments: json.RawMessage(`{"mood":4}`)}}),
		ai.ToolResultMessage([]ai.ToolResult{{ID: "toolu_2", Content: "logged"}}),
	}})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	thinking, _ := api.body(t, 0)["thinking"].(map[string]any)
	if thinking["type"] != "disabled" {
		t.Errorf("thinking = %v, want disabled for a call whose thinking was not kept", api.body(t, 0)["thinking"])
	}
}
```

Add to `messages_test.go`:

```go
func sentAt(t *testing.T, api *fakeAPI, i int) []map[string]any {
	t.Helper()
	raw, _ := api.body(t, i)["messages"].([]any)
	out := make([]map[string]any, len(raw))
	for j, m := range raw {
		out[j] = m.(map[string]any)
	}
	return out
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/ai/anthropic/ -run 'Thinking|Replayed'`
Expected: FAIL — `ProviderState` undefined.

- [ ] **Step 4: Write the implementation**

`internal/ai/client.go`, on `Message`:

```go
	// ProviderState is opaque data the client that produced this turn needs
	// to see again, such as Anthropic's thinking blocks, which must precede a
	// replayed tool call unchanged. Owned by that client; every other client
	// ignores it. Not persisted: a turn rebuilt from the database has none.
	ProviderState json.RawMessage
```

and on `StreamChunk`:

```go
	// ProviderState rides on the ToolCalls chunk; see Message.ProviderState.
	ProviderState json.RawMessage
```

(`encoding/json` import).

`internal/ai/anthropic/stream.go`: when emitting the tool-call chunk, attach thinking state:

```go
		if calls := toolCalls(msg.Content); len(calls) > 0 {
			if !send(ctx, out, ai.StreamChunk{ToolCalls: calls, ProviderState: thinkingState(msg.Content)}) {
				return
			}
		}
```

and add to `messages.go`:

```go
// thinkingBlock is the part of a thinking or redacted-thinking block the API
// needs back. Serialised into ProviderState and nowhere else.
type thinkingBlock struct {
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	Redacted  string `json:"redacted,omitempty"`
}

func thinkingState(content []sdk.ContentBlockUnion) json.RawMessage {
	var kept []thinkingBlock
	for _, block := range content {
		switch b := block.AsAny().(type) {
		case sdk.ThinkingBlock:
			kept = append(kept, thinkingBlock{Thinking: b.Thinking, Signature: b.Signature})
		case sdk.RedactedThinkingBlock:
			kept = append(kept, thinkingBlock{Redacted: b.Data})
		}
	}
	if len(kept) == 0 {
		return nil
	}
	raw, _ := json.Marshal(kept)
	return raw
}

func thinkingBlocks(state json.RawMessage) []sdk.ContentBlockParamUnion {
	var kept []thinkingBlock
	if len(state) == 0 || json.Unmarshal(state, &kept) != nil {
		return nil
	}
	out := make([]sdk.ContentBlockParamUnion, 0, len(kept))
	for _, k := range kept {
		if k.Redacted != "" {
			out = append(out, sdk.NewRedactedThinkingBlock(k.Redacted))
		} else {
			out = append(out, sdk.NewThinkingBlock(k.Signature, k.Thinking))
		}
	}
	return out
}
```

In `contentBlocks`, before the tool-call loop:

```go
	if len(m.ToolCalls) > 0 {
		blocks = append(thinkingBlocks(m.ProviderState), blocks...)
	}
```

In `client.go` `params`, after messages:

```go
	// A tool call rebuilt from the database has lost its thinking, and a
	// replayed call without it is refused. That one request runs without
	// thinking; everything else keeps the model's default.
	if lostThinking(req.Messages) {
		p.Thinking = sdk.ThinkingConfigParamUnion{OfDisabled: &sdk.ThinkingConfigDisabledParam{}}
	}
```

and in `messages.go`:

```go
func lostThinking(in []ai.Message) bool {
	for _, m := range in {
		if len(m.ToolCalls) > 0 && len(m.ProviderState) == 0 {
			return true
		}
	}
	return false
}
```

`internal/coach/service.go`, in `pump`: capture state with the calls and put it on the message.

```go
		var calls []ai.ToolCall
		var providerState json.RawMessage
```

inside the chunk loop:

```go
			if len(chunk.ToolCalls) > 0 {
				calls = append(calls, chunk.ToolCalls...)
				providerState = chunk.ProviderState
				continue
			}
```

and where the request grows:

```go
		callMsg := ai.ToolCallMessage(calls)
		callMsg.ProviderState = providerState
		request.Messages = append(request.Messages,
			callMsg,
			ai.ToolResultMessage(results),
		)
```

- [ ] **Step 5: Write the coach test**

Append to `internal/coach/tools_test.go`:

```go
// The coach hands a provider's opaque state back with the tool call it came
// from, so a provider that needs its thinking replayed gets it.
func TestProviderStateTravelsWithTheToolCall(t *testing.T) {
	t.Parallel()

	tools := &stubTools{
		tools:    []ai.Tool{searchTool},
		results:  map[string]string{"search_exercises": "- barbell-full-squat"},
		readOnly: map[string]bool{"search_exercises": true},
	}
	client := &fake.Client{Responses: []fake.Response{
		{ToolCalls: []ai.ToolCall{fake.ToolCall("search_exercises", `{"query":"squat"}`)}, ProviderState: json.RawMessage(`[{"signature":"s"}]`)},
		{Text: "Barbell full squat."},
	}}
	h := newToolHarness(t, client, tools)
	conversationID := newConversation(t, h)

	stream, err := h.coach.SendMessage(context.Background(), h.user, conversationID, "squat?")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, drainErr := drain(stream); drainErr != nil {
		t.Fatalf("drain: %v", drainErr)
	}

	second := client.Calls()[1]
	var found bool
	for _, m := range second.Messages {
		if len(m.ToolCalls) > 0 && string(m.ProviderState) == `[{"signature":"s"}]` {
			found = true
		}
	}
	if !found {
		t.Error("the follow-up request lost the provider state of the tool call")
	}
}
```

This needs the fake to carry state. In `internal/ai/fake/fake.go`, add to `Response`:

```go
	// ProviderState is emitted with ToolCalls, standing in for a provider
	// that needs opaque data replayed with its calls.
	ProviderState json.RawMessage
```

and in `Chat`, change the tool-call send to:

```go
			if !send(ctx, out, ai.StreamChunk{ToolCalls: resp.ToolCalls, ProviderState: resp.ProviderState}) {
```

`client.Calls()` already returns every request the fake received, in order. Add `internal/ai/fake/fake.go` to this task's files.

- [ ] **Step 6: Run the tests**

Run: `go test ./internal/ai/... && TEST_DATABASE_URL='postgres://north:north@localhost:5434/north?sslmode=disable' go test ./internal/coach/`
Expected: PASS. (Start local Postgres first: `orb start && docker-compose up -d postgres`.)

- [ ] **Step 7: Commit**

```bash
git add internal/ai internal/coach
git commit -m "ai/anthropic: replay thinking with tool calls; carry provider state through the coach"
```

---

### Task 7: Catalogue entry, client construction, key verification

**Files:**
- Modify: `internal/ai/providers/catalog.go` (entry; `KeyHeader`, `VerifyHeaders` fields; build the Anthropic client in `User`)
- Modify: `internal/aicreds/verify.go` (honour `KeyHeader`, `VerifyHeaders`)
- Test: `internal/ai/providers/catalog_test.go` (create if absent), `internal/aicreds/verify_test.go`

**Interfaces:**
- Consumes: `anthropic.New(anthropic.Options{...})`.
- Produces: `providers.BYOProvider.KeyHeader string` (empty = `Authorization: Bearer`), `providers.BYOProvider.VerifyHeaders map[string]string`; catalogue entry `anthropic`.

- [ ] **Step 1: Write the failing tests**

`internal/ai/providers/catalog_test.go`:

```go
package providers_test

import (
	"context"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai/providers"
)

func TestAnthropicIsAProviderAKeyCanBeBroughtFor(t *testing.T) {
	entry, ok := providers.ByName("anthropic")
	if !ok {
		t.Fatal("anthropic is not in the catalogue")
	}
	if entry.DefaultModel != "claude-opus-5" || entry.KeyHeader != "x-api-key" || entry.VerifyPath != "/v1/models" {
		t.Errorf("entry = %+v", entry)
	}

	client, err := providers.User(context.Background(), providers.UserSpec{Provider: "anthropic", APIKey: "sk-ant-test"})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if client.Name() != "anthropic" {
		t.Errorf("built %q", client.Name())
	}
}
```

Append to `internal/aicreds/verify_test.go`:

```go
// Anthropic takes its key in x-api-key and wants a version header; the bearer
// form it would otherwise get is a 401 for any key, good or bad.
func TestHTTPVerifierUsesTheProvidersKeyHeader(t *testing.T) {
	var gotKey, gotBearer, gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotBearer, gotVersion = r.Header.Get("x-api-key"), r.Header.Get("Authorization"), r.Header.Get("anthropic-version")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	entry := providers.BYOProvider{
		Name: "anthropic", BaseURL: srv.URL, VerifyPath: "/v1/models",
		KeyHeader: "x-api-key", VerifyHeaders: map[string]string{"anthropic-version": "2023-06-01"},
	}
	if err := aicreds.NewHTTPVerifier(srv.Client()).Verify(context.Background(), entry, testKey); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if gotKey != testKey || gotBearer != "" || gotVersion != "2023-06-01" {
		t.Errorf("x-api-key=%q Authorization=%q anthropic-version=%q", gotKey, gotBearer, gotVersion)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/ai/providers/ ./internal/aicreds/ -run 'Anthropic|KeyHeader'`
Expected: FAIL — no `KeyHeader` field.

- [ ] **Step 3: Write the implementation**

`internal/ai/providers/catalog.go`, fields on `BYOProvider`:

```go
	// KeyHeader names the header the key goes in. Empty means the OpenAI
	// convention, "Authorization: Bearer <key>". Anthropic uses x-api-key.
	KeyHeader string

	// VerifyHeaders are sent with the verification request as-is, for a
	// provider that refuses one without them (Anthropic's version header).
	VerifyHeaders map[string]string
```

Catalogue entry, after Gemini:

```go
	{
		// Native Messages API through the official SDK; see internal/ai/anthropic.
		Name: "anthropic", Label: "Anthropic (Claude)",
		BaseURL: "https://api.anthropic.com", DefaultModel: "claude-opus-5",
		KeyHint: "sk-ant-…", VerifyPath: "/v1/models", KeyHeader: "x-api-key",
		VerifyHeaders: map[string]string{"anthropic-version": "2023-06-01"},
		Note: "Claude on your own Anthropic account. Billed to that account.",
	},
```

Update the `ByName` comment: remove "Anthropic is deliberately absent…" and replace with one line: "Anthropic speaks its own dialect, so it has its own client (internal/ai/anthropic) rather than going through openaicompat."

In `User`, next to the Gemini branch:

```go
	if entry.Name == "anthropic" {
		client, err := anthropic.New(anthropic.Options{
			APIKey: spec.APIKey, DefaultModel: model, HTTPClient: spec.HTTPClient,
		})
		if err != nil {
			// Not wrapped with the spec: it holds the key.
			return nil, fmt.Errorf("providers: cannot build an anthropic client for this credential")
		}
		return ai.Metered(client, spec.Meter, true), nil
	}
```

(import `github.com/NorthAIProject/north-client/internal/ai/anthropic`).

`internal/aicreds/verify.go`, replace the single `req.Header.Set("Authorization", ...)`:

```go
	if entry.KeyHeader != "" {
		req.Header.Set(entry.KeyHeader, key)
	} else {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	for name, value := range entry.VerifyHeaders {
		req.Header.Set(name, value)
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/ai/providers/ ./internal/aicreds/ ./web/settings/`
Expected: PASS (the settings page lists the new entry from the catalogue; if a settings golden/snapshot test lists providers, update it to include "Anthropic (Claude)").

- [ ] **Step 5: Commit**

```bash
git add internal/ai/providers internal/aicreds web/settings
git commit -m "providers: Anthropic as a bring-your-own-key provider"
```

---

### Task 8: Live smoke test and full suite

**Files:**
- Create: `internal/ai/anthropic/live_test.go`

**Interfaces:**
- Consumes: the whole package.

- [ ] **Step 1: Write the live test**

`internal/ai/anthropic/live_test.go`:

```go
package anthropic_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/anthropic"
)

// One real tool round trip, the shape every coach turn about a movement takes.
// Skipped without a key, so it costs nothing in CI.
func TestLiveToolRoundTrip(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY not set; skipping live test")
	}
	client, err := anthropic.New(anthropic.Options{APIKey: key})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	tool := ai.Tool{Name: "get_exercise", Description: "Read one exercise from the catalogue. Call it before describing a movement.",
		Parameters: ai.Object("which", map[string]*ai.Schema{"slug": ai.String("the exercise slug")}, "slug")}
	req := ai.Request{
		System:   "You are a coach. Always look an exercise up before describing it.",
		Messages: []ai.Message{ai.UserText("How do I do a barbell-full-squat?")},
		Tools:    []ai.Tool{tool},
	}

	calls, state := liveRound(t, ctx, client, req)
	if len(calls) == 0 {
		t.Fatal("the model did not call the tool")
	}
	callMsg := ai.ToolCallMessage(calls)
	callMsg.ProviderState = state
	req.Messages = append(req.Messages, callMsg, ai.ToolResultMessage([]ai.ToolResult{{
		ID: calls[0].ID, Name: calls[0].Name, Content: "Barbell Full Squat: bar on upper back, sit back and down, drive up.",
	}}))

	calls, _ = liveRound(t, ctx, client, req)
	if len(calls) != 0 {
		t.Logf("model asked again: %s", mustJSON(calls))
	}
}

func liveRound(t *testing.T, ctx context.Context, client *anthropic.Client, req ai.Request) ([]ai.ToolCall, json.RawMessage) {
	t.Helper()
	ch, err := client.Chat(ctx, req)
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	var calls []ai.ToolCall
	var state json.RawMessage
	for c := range ch {
		if c.Err != nil {
			t.Fatalf("stream: %v", c.Err)
		}
		if len(c.ToolCalls) > 0 {
			calls, state = c.ToolCalls, c.ProviderState
		}
		if c.Usage != nil {
			t.Logf("usage: %d in, %d out, model %s", c.Usage.InputTokens, c.Usage.OutputTokens, c.Model)
		}
	}
	return calls, state
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
```

- [ ] **Step 2: Run the suite**

Run: `go vet ./... && go test ./internal/ai/... ./internal/aicreds/ ./internal/ai/providers/`
Then, with local Postgres up: `TEST_DATABASE_URL='postgres://north:north@localhost:5434/north?sslmode=disable' go test ./...`
Expected: PASS; `TestLiveToolRoundTrip` SKIP without a key. Run `templ generate` first if `web/` packages fail on stale generated files.

- [ ] **Step 3: Run the live test if a key is available**

Run: `ANTHROPIC_API_KEY=... go test ./internal/ai/anthropic/ -run Live -v`
Expected: PASS, with usage logged for both rounds.

- [ ] **Step 4: Commit**

```bash
git add internal/ai/anthropic/live_test.go
git commit -m "ai/anthropic: live tool round trip, skipped without a key"
```
