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
	if len(req.Tools) > 0 {
		p.Tools = toTools(req.Tools)
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

// classify is filled in by Task 4.
func classify(err error) error { return err }

var _ ai.Client = (*Client)(nil)
