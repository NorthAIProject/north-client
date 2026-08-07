// Package openaicompat implements ai.Client against any service that speaks
// OpenAI's /chat/completions dialect.
//
// One implementation reaches OpenRouter, NVIDIA NIM, xAI, and a self-hosted
// Hermes gateway. What separates them is configuration — a base URL, a key, a
// model string, and a couple of headers — not code. The model string still
// carries the family where the upstream service multiplexes ("anthropic/
// claude-sonnet-4.5" on OpenRouter), so this remains several provider SDKs
// North does not have to depend on.
//
// Written against net/http rather than an SDK. The surface used here is one
// POST and an SSE parse; a dependency would add more than it removed.
package openaicompat

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

type Client struct {
	http               *http.Client
	name               string
	baseURL            string
	apiKey             string
	defaultModel       string
	headers            map[string]string
	supportsJSONSchema bool
}

type Options struct {
	// Name is the registry key and the label persisted against every message
	// this client answers. It must be unique across registered providers.
	Name string

	// BaseURL is the API root, up to and including the version segment —
	// "/chat/completions" is appended to it.
	BaseURL string

	APIKey       string
	DefaultModel string

	// Headers are sent on every request. OpenRouter's attribution pair
	// (HTTP-Referer, X-Title) rides here rather than in dedicated fields:
	// it is one provider's convention, not part of the dialect.
	Headers map[string]string

	// SupportsJSONSchema reports whether the service honours OpenAI's strict
	// response_format: json_schema. OpenRouter and xAI do. NVIDIA NIM and
	// Hermes vary by model, and a request they reject fails outright — so when
	// this is false the schema is asked for in the prompt instead. That is
	// weaker, and callers wanting structured output from such a provider need
	// to tolerate a malformed first attempt.
	SupportsJSONSchema bool

	HTTPClient *http.Client
}

func New(opts Options) (*Client, error) {
	if opts.Name == "" {
		return nil, apperr.Wrap(apperr.ErrValidation, "openaicompat: name is required")
	}
	if opts.BaseURL == "" {
		return nil, apperr.Wrap(apperr.ErrValidation, "openaicompat: %s: base URL is required", opts.Name)
	}
	if opts.APIKey == "" {
		return nil, apperr.Wrap(apperr.ErrValidation, "openaicompat: %s: API key is required", opts.Name)
	}
	if opts.DefaultModel == "" {
		return nil, apperr.Wrap(apperr.ErrValidation, "openaicompat: %s: default model is required", opts.Name)
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

	headers := make(map[string]string, len(opts.Headers))
	for k, v := range opts.Headers {
		if v != "" {
			headers[k] = v
		}
	}

	return &Client{
		http:               httpClient,
		name:               opts.Name,
		baseURL:            strings.TrimSuffix(opts.BaseURL, "/"),
		apiKey:             opts.APIKey,
		defaultModel:       opts.DefaultModel,
		headers:            headers,
		supportsJSONSchema: opts.SupportsJSONSchema,
	}, nil
}

func (c *Client) Name() string { return c.name }

func (c *Client) Generate(ctx context.Context, req ai.Request) (*ai.Response, error) {
	resp, err := c.post(ctx, c.body(req, false))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var payload completionResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, apperr.Wrap(err, "%s: decode response", c.name)
	}
	if len(payload.Choices) == 0 {
		return nil, apperr.Wrap(apperr.ErrUnavailable, "%s: response contained no choices", c.name)
	}

	return &ai.Response{
		Text:         payload.Choices[0].Message.Content,
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

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())

			// Blank lines separate frames; comment lines (": OPENROUTER
			// PROCESSING" and friends) are keep-alives sent while a slow model
			// warms up.
			if line == "" || strings.HasPrefix(line, ":") {
				continue
			}
			data, found := strings.CutPrefix(line, "data: ")
			if !found {
				continue
			}
			if data == "[DONE]" {
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
				if choice.Delta.Content == "" {
					continue
				}
				if !send(ctx, out, ai.StreamChunk{Text: choice.Delta.Content}) {
					return
				}
			}
		}

		if err := scanner.Err(); err != nil {
			send(ctx, out, ai.StreamChunk{Err: apperr.Wrap(err, "%s: read stream", c.name)})
		}
	}()

	return out, nil
}

// UploadFile has no counterpart in the OpenAI chat dialect, which takes
// attachments inline.
//
// The error is explicit rather than a silent empty URI so a caller routes video
// work to a provider that does support uploads — in North, Gemini.
func (c *Client) UploadFile(ctx context.Context, req ai.UploadRequest) (*ai.File, error) {
	return nil, apperr.Wrap(apperr.ErrValidation,
		"%s: no file upload API; send small media inline or use a provider that supports uploads", c.name)
}

func (c *Client) post(ctx context.Context, body any) (*http.Response, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, apperr.Wrap(err, "%s: encode request", c.name)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(encoded))
	if err != nil {
		return nil, apperr.Wrap(err, "%s: build request", c.name)
	}

	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range c.headers {
		httpReq.Header.Set(k, v)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		// Both causes are kept in the chain. The sentinel is what handlers
		// match on; the transport error underneath is how a caller tells a
		// provider that fell over from a user who closed the tab, since a
		// cancelled context arrives here looking like any other failure.
		return nil, fmt.Errorf("%w: %s: %w", apperr.ErrUnavailable, c.name, err)
	}

	if resp.StatusCode >= 300 {
		// Bounded read: an error page is not worth unbounded memory.
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		return nil, c.statusError(resp.StatusCode, string(detail))
	}

	return resp, nil
}

func (c *Client) body(req ai.Request, stream bool) map[string]any {
	model := req.Model
	if model == "" {
		model = c.defaultModel
	}

	system := req.System
	if req.ResponseSchema != nil && !c.supportsJSONSchema {
		system = withSchemaInstruction(system, req.ResponseSchema)
	}

	messages := make([]map[string]any, 0, len(req.Messages)+1)
	if system != "" {
		messages = append(messages, map[string]any{"role": "system", "content": system})
	}
	for _, m := range req.Messages {
		messages = append(messages, map[string]any{
			"role":    toRole(m.Role),
			"content": m.Text(),
		})
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
	if req.ResponseSchema != nil && c.supportsJSONSchema {
		body["response_format"] = map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "response",
				"strict": true,
				"schema": toJSONSchema(req.ResponseSchema),
			},
		}
	}
	if stream {
		// Otherwise the final usage frame is omitted and North cannot account
		// for what a conversation cost.
		body["stream_options"] = map[string]any{"include_usage": true}
	}

	return body
}

// withSchemaInstruction asks for a shape in words, for providers that cannot be
// held to one by the API.
//
// response_format is left off entirely rather than downgraded to json_object:
// that value is itself unevenly supported, and a rejected request is worse than
// a reply that merely needs re-parsing. Callers already retry bad output —
// see the repair loop in internal/workouts.
func withSchemaInstruction(system string, schema *ai.Schema) string {
	encoded, err := json.Marshal(toJSONSchema(schema))
	if err != nil {
		return system
	}

	instruction := "Respond with a single JSON object and nothing else. No prose, no code fence. " +
		"It must validate against this JSON Schema:\n" + string(encoded)

	if system == "" {
		return instruction
	}
	return system + "\n\n" + instruction
}

// toRole maps North's vocabulary onto the OpenAI one.
func toRole(r ai.Role) string {
	if r == ai.RoleModel {
		return "assistant"
	}
	return "user"
}

// toJSONSchema renders an ai.Schema as standard JSON Schema.
//
// additionalProperties is false throughout because OpenAI-compatible strict
// mode requires it; without it the request is rejected outright.
func toJSONSchema(s *ai.Schema) map[string]any {
	if s == nil {
		return nil
	}

	out := map[string]any{"type": string(s.Type)}
	if s.Description != "" {
		out["description"] = s.Description
	}
	if len(s.Enum) > 0 {
		out["enum"] = s.Enum
	}
	if s.Items != nil {
		out["items"] = toJSONSchema(s.Items)
	}
	if len(s.Properties) > 0 {
		props := make(map[string]any, len(s.Properties))
		for name, prop := range s.Properties {
			props[name] = toJSONSchema(prop)
		}
		out["properties"] = props
		out["additionalProperties"] = false

		required := s.Required
		if len(required) == 0 {
			// Strict mode requires every property to be listed as required.
			required = make([]string, 0, len(s.Properties))
			for name := range s.Properties {
				required = append(required, name)
			}
		}
		out["required"] = required
	}

	return out
}

func (c *Client) statusError(status int, detail string) error {
	detail = strings.TrimSpace(detail)
	switch {
	// Billing is its own outcome: unlike an overload, waiting will not fix it,
	// and unlike a bad key it says nothing about the credential. A chain of
	// providers needs to tell those apart to pick a useful next move.
	case status == http.StatusPaymentRequired:
		return fmt.Errorf("%w: %s returned %d: %s", apperr.ErrPaymentRequired, c.name, status, detail)
	case status == http.StatusTooManyRequests, status >= 500:
		return fmt.Errorf("%w: %s returned %d: %s", apperr.ErrUnavailable, c.name, status, detail)
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return fmt.Errorf("%w: %s returned %d: %s", apperr.ErrForbidden, c.name, status, detail)
	default:
		return fmt.Errorf("%s returned %d: %s", c.name, status, detail)
	}
}

type completionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage usagePayload `json:"usage"`
}

type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
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
