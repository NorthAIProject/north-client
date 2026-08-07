// Package openrouter implements ai.Client against OpenRouter's
// OpenAI-compatible API.
//
// One implementation reaches Claude, GPT, Grok, DeepSeek, and the rest: the
// model string carries the family ("anthropic/claude-sonnet-4.5"). That is
// three or four provider SDKs North does not have to depend on, and the second
// implementation the ai.Client interface needs in order to be proven rather
// than merely asserted.
//
// Written against net/http rather than an SDK. The surface used here is one
// POST and an SSE parse; a dependency would add more than it removed.
package openrouter

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

const defaultBaseURL = "https://openrouter.ai/api/v1"

type Client struct {
	http         *http.Client
	baseURL      string
	apiKey       string
	defaultModel string
	siteURL      string
	siteName     string
}

type Options struct {
	APIKey       string
	DefaultModel string

	// SiteURL and SiteName populate OpenRouter's attribution headers. Optional.
	SiteURL  string
	SiteName string

	// BaseURL overrides the endpoint, for tests.
	BaseURL string

	HTTPClient *http.Client
}

func New(opts Options) (*Client, error) {
	if opts.APIKey == "" {
		return nil, apperr.Wrap(apperr.ErrValidation, "openrouter: API key is required")
	}

	httpClient := opts.HTTPClient
	if httpClient == nil {
		// No overall timeout: a streamed coaching reply can legitimately run
		// for minutes. Request lifetime is bounded by the caller's context.
		httpClient = &http.Client{
			Transport: &http.Transport{
				ResponseHeaderTimeout: 60 * time.Second,
				IdleConnTimeout:       90 * time.Second,
			},
		}
	}

	baseURL := opts.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	model := opts.DefaultModel
	if model == "" {
		model = "anthropic/claude-sonnet-4.5"
	}

	return &Client{
		http:         httpClient,
		baseURL:      strings.TrimSuffix(baseURL, "/"),
		apiKey:       opts.APIKey,
		defaultModel: model,
		siteURL:      opts.SiteURL,
		siteName:     opts.SiteName,
	}, nil
}

func (c *Client) Name() string { return "openrouter" }

func (c *Client) Generate(ctx context.Context, req ai.Request) (*ai.Response, error) {
	resp, err := c.post(ctx, c.body(req, false))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var payload completionResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, apperr.Wrap(err, "openrouter: decode response")
	}
	if len(payload.Choices) == 0 {
		return nil, apperr.Wrap(apperr.ErrUnavailable, "openrouter: response contained no choices")
	}

	return &ai.Response{
		Text:         payload.Choices[0].Message.Content,
		ToolCalls:    fromToolCallPayload(payload.Choices[0].Message.ToolCalls),
		FinishReason: payload.Choices[0].FinishReason,
		Usage: ai.Usage{
			InputTokens:  payload.Usage.PromptTokens,
			OutputTokens: payload.Usage.CompletionTokens,
		},
	}, nil
}

func (c *Client) Chat(ctx context.Context, req ai.Request) (<-chan ai.StreamChunk, error) {
	resp, err := c.post(ctx, c.body(req, true))
	if err != nil {
		return nil, err
	}

	out := make(chan ai.StreamChunk)

	go func() {
		defer close(out)
		defer func() { _ = resp.Body.Close() }()

		scanner := bufio.NewScanner(resp.Body)
		// A single SSE frame can carry a large delta; the default 64KB limit
		// would turn that into a spurious "token too long" mid-answer.
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		// Tool calls arrive in fragments: the first delta for an index carries
		// the id and name, and the argument JSON is appended across the ones
		// that follow. They are accumulated by index and emitted once whole,
		// because half an argument object cannot be acted on.
		accumulator := newToolCallAccumulator()
		flushed := false
		flush := func() bool {
			if flushed {
				return true
			}
			flushed = true
			calls := accumulator.calls()
			if len(calls) == 0 {
				return true
			}
			return send(ctx, out, ai.StreamChunk{ToolCalls: calls})
		}

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())

			// Blank lines separate frames; ": OPENROUTER PROCESSING" comments
			// are keep-alives sent while a slow model warms up.
			if line == "" || strings.HasPrefix(line, ":") {
				continue
			}
			data, found := strings.CutPrefix(line, "data: ")
			if !found {
				continue
			}
			if data == "[DONE]" {
				flush()
				return
			}

			var chunk streamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				// A frame we cannot parse is not worth killing the answer over.
				continue
			}

			if chunk.Usage != nil {
				usage := ai.Usage{
					InputTokens:  chunk.Usage.PromptTokens,
					OutputTokens: chunk.Usage.CompletionTokens,
				}
				if !send(ctx, out, ai.StreamChunk{Usage: &usage}) {
					return
				}
			}

			for _, choice := range chunk.Choices {
				accumulator.add(choice.Delta.ToolCalls)

				if choice.Delta.Content == "" {
					continue
				}
				if !send(ctx, out, ai.StreamChunk{Text: choice.Delta.Content}) {
					return
				}
			}
		}

		if err := scanner.Err(); err != nil {
			send(ctx, out, ai.StreamChunk{Err: apperr.Wrap(err, "openrouter: read stream")})
			return
		}

		// Reached when the stream ends without a [DONE] frame.
		flush()
	}()

	return out, nil
}

// UploadFile has no counterpart on OpenRouter, which takes attachments inline.
//
// It returns an empty URI rather than an error so a caller can fall back to
// inlining the bytes. Video analysis should route to Gemini regardless.
func (c *Client) UploadFile(ctx context.Context, req ai.UploadRequest) (*ai.File, error) {
	return nil, apperr.Wrap(apperr.ErrValidation,
		"openrouter: no file upload API; send small media inline or use a provider that supports uploads")
}

func (c *Client) post(ctx context.Context, body any) (*http.Response, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, apperr.Wrap(err, "openrouter: encode request")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(encoded))
	if err != nil {
		return nil, apperr.Wrap(err, "openrouter: build request")
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	if c.siteURL != "" {
		httpReq.Header.Set("HTTP-Referer", c.siteURL)
	}
	if c.siteName != "" {
		httpReq.Header.Set("X-Title", c.siteName)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, apperr.Wrap(apperr.ErrUnavailable, "openrouter: %v", err)
	}

	if resp.StatusCode >= 300 {
		// Bounded read: an error page is not worth unbounded memory.
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		return nil, statusError(resp.StatusCode, string(detail))
	}

	return resp, nil
}

func (c *Client) body(req ai.Request, stream bool) map[string]any {
	model := req.Model
	if model == "" {
		model = c.defaultModel
	}

	messages := make([]map[string]any, 0, len(req.Messages)+1)
	if req.System != "" {
		messages = append(messages, map[string]any{"role": "system", "content": req.System})
	}
	for _, m := range req.Messages {
		// A turn is content, tool calls, or tool results. The OpenAI shape
		// gives each its own form: an assistant turn carrying tool_calls, and
		// one message per result with role "tool".
		switch {
		case len(m.ToolCalls) > 0:
			messages = append(messages, map[string]any{
				"role":       "assistant",
				"content":    nil,
				"tool_calls": toToolCallPayload(m.ToolCalls),
			})
		case len(m.ToolResults) > 0:
			for _, result := range m.ToolResults {
				messages = append(messages, map[string]any{
					"role":         "tool",
					"tool_call_id": result.ID,
					"content":      result.Content,
				})
			}
		default:
			messages = append(messages, map[string]any{
				"role":    toRole(m.Role),
				"content": m.Text(),
			})
		}
	}

	body := map[string]any{
		"model":    model,
		"messages": messages,
		"stream":   stream,
	}

	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.ResponseSchema != nil {
		body["response_format"] = map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "response",
				"strict": true,
				"schema": ai.JSONSchema(req.ResponseSchema),
			},
		}
	}
	if len(req.Tools) > 0 {
		body["tools"] = toToolPayload(req.Tools)
	}
	if stream {
		// Otherwise the final usage frame is omitted and North cannot account
		// for what a conversation cost.
		body["stream_options"] = map[string]any{"include_usage": true}
	}

	return body
}

func toToolPayload(tools []ai.Tool) []map[string]any {
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  ai.JSONSchema(tool.Parameters),
			},
		})
	}
	return out
}

func toToolCallPayload(calls []ai.ToolCall) []map[string]any {
	out := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		out = append(out, map[string]any{
			"id":   call.ID,
			"type": "function",
			"function": map[string]any{
				"name":      call.Name,
				"arguments": string(call.Arguments),
			},
		})
	}
	return out
}

// fromToolCallPayload converts the wire form back.
//
// Arguments arrive as a JSON string containing JSON, which is the OpenAI
// convention rather than a mistake. An empty one becomes "{}" so a caller can
// always unmarshal without a nil check.
func fromToolCallPayload(payload []toolCallPayload) []ai.ToolCall {
	if len(payload) == 0 {
		return nil
	}

	calls := make([]ai.ToolCall, 0, len(payload))
	for _, call := range payload {
		arguments := call.Function.Arguments
		if arguments == "" {
			arguments = "{}"
		}
		calls = append(calls, ai.ToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			Arguments: json.RawMessage(arguments),
		})
	}
	return calls
}

// toRole maps North's vocabulary onto the OpenAI one.
func toRole(r ai.Role) string {
	if r == ai.RoleModel {
		return "assistant"
	}
	return "user"
}

func statusError(status int, detail string) error {
	detail = strings.TrimSpace(detail)
	switch {
	case status == http.StatusTooManyRequests, status >= 500:
		return fmt.Errorf("%w: openrouter returned %d: %s", apperr.ErrUnavailable, status, detail)
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return fmt.Errorf("%w: openrouter returned %d: %s", apperr.ErrForbidden, status, detail)
	default:
		return fmt.Errorf("openrouter returned %d: %s", status, detail)
	}
}

// toolCallPayload is the OpenAI tool-call wire shape, shared by the complete
// and streamed responses.
type toolCallPayload struct {
	// Index orders the calls when they arrive in stream deltas; a single
	// response carries them in order already.
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type completionResponse struct {
	Choices []struct {
		Message struct {
			Content   string            `json:"content"`
			ToolCalls []toolCallPayload `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage usagePayload `json:"usage"`
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content   string            `json:"content"`
			ToolCalls []toolCallPayload `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *usagePayload `json:"usage"`
}

type usagePayload struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

func send(ctx context.Context, out chan<- ai.StreamChunk, chunk ai.StreamChunk) bool {
	select {
	case out <- chunk:
		return true
	case <-ctx.Done():
		return false
	}
}

var _ ai.Client = (*Client)(nil)

// toolCallAccumulator reassembles tool calls from streamed fragments.
//
// OpenAI-compatible streams send the id and name once, on the first delta for
// an index, then append the argument JSON across later deltas. Keyed by index
// rather than id for that reason: the id is not present on every fragment.
type toolCallAccumulator struct {
	order   []int
	byIndex map[int]*ai.ToolCall
	args    map[int]*strings.Builder
}

func newToolCallAccumulator() *toolCallAccumulator {
	return &toolCallAccumulator{
		byIndex: map[int]*ai.ToolCall{},
		args:    map[int]*strings.Builder{},
	}
}

func (a *toolCallAccumulator) add(fragments []toolCallPayload) {
	for _, fragment := range fragments {
		call, seen := a.byIndex[fragment.Index]
		if !seen {
			call = &ai.ToolCall{}
			a.byIndex[fragment.Index] = call
			a.args[fragment.Index] = &strings.Builder{}
			a.order = append(a.order, fragment.Index)
		}

		if fragment.ID != "" {
			call.ID = fragment.ID
		}
		if fragment.Function.Name != "" {
			call.Name = fragment.Function.Name
		}
		a.args[fragment.Index].WriteString(fragment.Function.Arguments)
	}
}

func (a *toolCallAccumulator) calls() []ai.ToolCall {
	if len(a.order) == 0 {
		return nil
	}

	out := make([]ai.ToolCall, 0, len(a.order))
	for _, index := range a.order {
		call := *a.byIndex[index]
		arguments := a.args[index].String()
		if arguments == "" {
			arguments = "{}"
		}
		call.Arguments = json.RawMessage(arguments)
		out = append(out, call)
	}
	return out
}
