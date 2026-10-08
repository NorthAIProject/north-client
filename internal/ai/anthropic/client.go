// Package anthropic is Khepri's client for Claude, over Anthropic's native
// Messages API.
//
// Reached two ways: through a key a person brings themselves (see
// providers.Catalog), and as Khepri's own provider when ANTHROPIC_API_KEY is
// set (Managed, built in providers.Build). It calls Khepri's tools natively,
// so it does not implement ai.ToolCaller: the coach runs its own tool loop
// against it, and the MCP bridge built for gateways never applies.
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

	// Effort is sent as output_config.effort. Empty leaves the model default.
	Effort string

	// Thinking is "adaptive" or "disabled". Empty sends nothing, which is the
	// model default and what a person's own key has always had.
	Thinking string

	// Managed marks Khepri's own client, the first rung of a chain rather than
	// a key a person brought. It changes two things. A model name meant for
	// another provider (AI_FAST_MODEL is an OpenRouter slug) is replaced with
	// DefaultModel instead of being sent to a 404. And every 4xx fails over,
	// because the chain behind it is the one that has always answered: a
	// request Claude rejects is not evidence that the request is malformed.
	Managed bool
}

type Client struct {
	sdk          sdk.Client
	defaultModel string
	effort       sdk.OutputConfigEffort
	thinking     string
	managed      bool
}

// minThinkingTokens is the smallest max_tokens worth thinking under. Thinking
// counts toward the limit, so a short request that thinks first can spend the
// whole budget before it writes a word.
const minThinkingTokens = 1024

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
	return &Client{
		sdk:          sdk.NewClient(reqOpts...),
		defaultModel: model,
		effort:       sdk.OutputConfigEffort(opts.Effort),
		thinking:     opts.Thinking,
		managed:      opts.Managed,
	}, nil
}

func (c *Client) Name() string { return "anthropic" }

func (c *Client) Generate(ctx context.Context, req ai.Request) (*ai.Response, error) {
	msg, err := c.sdk.Messages.New(ctx, c.params(req))
	if err != nil {
		return nil, c.classify(err)
	}
	if err := stopError(msg.StopReason, msg.Content); err != nil {
		return nil, err
	}
	return fromMessage(msg), nil
}

// UploadFile returns an empty URI: images go inline as base64 blocks, which
// every caller already does for providers with no upload step.
func (c *Client) UploadFile(context.Context, ai.UploadRequest) (*ai.File, error) {
	return &ai.File{}, nil
}

func (c *Client) params(req ai.Request) sdk.MessageNewParams {
	model := req.Model
	// The managed client shares callers with the rest of the chain, and their
	// model names are written for OpenRouter. Only a Claude name means
	// anything here.
	if model == "" || (c.managed && !strings.HasPrefix(model, "claude-")) {
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
	if len(req.Tools) > 0 {
		p.Tools = toTools(req.Tools)
	}
	// A tool call in this turn with no record of the turn that made it (a row
	// from before provider_state, or another provider's call) cannot have its
	// thinking replayed, and a replay without it is refused. That request runs
	// without thinking. So does one too short to think in, or any request when
	// thinking is configured off. Otherwise thinking is what was configured,
	// and with nothing configured (a personal key) the model's default.
	switch {
	case lostThinking(req.Messages), c.thinking == "disabled",
		c.thinking != "" && maxTokens < minThinkingTokens:
		p.Thinking = sdk.ThinkingConfigParamUnion{OfDisabled: &sdk.ThinkingConfigDisabledParam{}}
	case c.thinking == "adaptive":
		p.Thinking = sdk.ThinkingConfigParamUnion{OfAdaptive: &sdk.ThinkingConfigAdaptiveParam{}}
	}
	// Effort and format share output_config; each is set on its own field so
	// neither overwrites the other.
	p.OutputConfig.Effort = c.effort
	if req.ResponseSchema != nil {
		p.OutputConfig.Format = sdk.JSONOutputFormatParam{Schema: ai.JSONSchema(req.ResponseSchema)}
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
	resp.ToolCalls = toolCalls(msg.Content)
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

var _ ai.Client = (*Client)(nil)
