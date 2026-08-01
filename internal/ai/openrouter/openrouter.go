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
	defer resp.Body.Close()

	var payload completionResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, apperr.Wrap(err, "openrouter: decode response")
	}
	if len(payload.Choices) == 0 {
		return nil, apperr.Wrap(apperr.ErrUnavailable, "openrouter: response contained no choices")
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
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		// A single SSE frame can carry a large delta; the default 64KB limit
		// would turn that into a spurious "token too long" mid-answer.
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

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
			send(ctx, out, ai.StreamChunk{Err: apperr.Wrap(err, "openrouter: read stream")})
		}
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
		resp.Body.Close()
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
	if req.ResponseSchema != nil {
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
