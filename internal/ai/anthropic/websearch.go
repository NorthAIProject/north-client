package anthropic

import (
	"encoding/json"
	"errors"
	"strings"

	sdk "github.com/anthropics/anthropic-sdk-go"

	"github.com/NorthAIProject/north-client/internal/ai"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// maxContinuations bounds how often one reply is resumed after the API paused
// it mid-search. A pause is the API's own time limit on a long turn, not a
// failure, and a reply needing more than a few has stopped being a reply.
const maxContinuations = 3

// searches reports whether a request gets the web search tool: the caller
// allowed it, the deployment switched it on, and the key is Khepri's own. A
// person's own key is their bill, and a search is charged apart from tokens,
// so it is never spent on their behalf.
func (c *Client) searches(req ai.Request) bool {
	return req.AllowWebSearch && c.managed && c.webSearchMaxUses > 0 && req.ResponseSchema == nil
}

// webSearchTool is the basic search tool, capped per request. The basic
// version on purpose: the later ones filter results by running code, which
// Haiku is not documented to support and which is not covered by zero data
// retention.
func (c *Client) webSearchTool() sdk.ToolUnionParam {
	return sdk.ToolUnionParam{OfWebSearchTool20250305: &sdk.WebSearchTool20250305Param{
		MaxUses: sdk.Int(int64(c.webSearchMaxUses)),
	}}
}

// refusedRequest reports an error that is the API rejecting the request
// itself, as classify words a 400. Billing and key failures are also 400s or
// near them, and are not this: asking again would only fail again.
func refusedRequest(err error) bool {
	return err != nil && strings.Contains(err.Error(), "anthropic returned 400") &&
		!errors.Is(err, apperr.ErrPaymentRequired) && !errors.Is(err, apperr.ErrForbidden)
}

// blockParam turns a block of a reply into the block that sends it back,
// unchanged. False for a block there is nothing to send back for: empty text,
// or a kind this client never asks for.
//
// Search blocks go back whole. A result's encrypted content and a citation's
// encrypted index are what let the model read its own search again, and the
// API refuses a turn that returns one without the other.
func blockParam(block sdk.ContentBlockUnion) (sdk.ContentBlockParamUnion, bool) {
	switch b := block.AsAny().(type) {
	case sdk.ThinkingBlock:
		return sdk.NewThinkingBlock(b.Signature, b.Thinking), true
	case sdk.RedactedThinkingBlock:
		return sdk.NewRedactedThinkingBlock(b.Data), true
	case sdk.TextBlock:
		if b.Text == "" {
			return sdk.ContentBlockParamUnion{}, false
		}
		text := sdk.TextBlockParam{Text: b.Text}
		for _, citation := range b.Citations {
			if c, ok := citation.AsAny().(sdk.CitationsWebSearchResultLocation); ok {
				text.Citations = append(text.Citations, sdk.TextCitationParamUnion{
					OfWebSearchResultLocation: &sdk.CitationWebSearchResultLocationParam{
						Title:          sdk.String(c.Title),
						CitedText:      c.CitedText,
						EncryptedIndex: c.EncryptedIndex,
						URL:            c.URL,
					},
				})
			}
		}
		return sdk.ContentBlockParamUnion{OfText: &text}, true
	case sdk.ToolUseBlock:
		return toolUse(ai.ToolCall{ID: b.ID, Name: b.Name, Arguments: json.RawMessage(b.JSON.Input.Raw())}), true
	case sdk.ServerToolUseBlock:
		var input any = map[string]any{}
		if raw := b.JSON.Input.Raw(); raw != "" {
			_ = json.Unmarshal([]byte(raw), &input)
		}
		return sdk.NewServerToolUseBlock(b.ID, input, sdk.ServerToolUseBlockParamName(b.Name)), true
	case sdk.WebSearchToolResultBlock:
		if b.Content.JSON.OfWebSearchResultBlockArray.Valid() {
			results := make([]sdk.WebSearchResultBlockParam, 0, len(b.Content.OfWebSearchResultBlockArray))
			for _, r := range b.Content.OfWebSearchResultBlockArray {
				result := sdk.WebSearchResultBlockParam{EncryptedContent: r.EncryptedContent, Title: r.Title, URL: r.URL}
				if r.PageAge != "" {
					result.PageAge = sdk.String(r.PageAge)
				}
				results = append(results, result)
			}
			return sdk.NewWebSearchToolResultBlock(results, b.ToolUseID), true
		}
		// A search that failed comes back as a result holding an error, with
		// a 200. It is sent back as it came: the model has already read it.
		return sdk.NewWebSearchToolResultBlock(sdk.WebSearchToolRequestErrorParam{ErrorCode: b.Content.ErrorCode}, b.ToolUseID), true
	}
	return sdk.ContentBlockParamUnion{}, false
}

// assistantTurn is a reply so far, as the message that hands it back to be
// carried on with.
func assistantTurn(content []sdk.ContentBlockUnion) sdk.MessageParam {
	blocks := make([]sdk.ContentBlockParamUnion, 0, len(content))
	for _, block := range content {
		if param, ok := blockParam(block); ok {
			blocks = append(blocks, param)
		}
	}
	return sdk.NewAssistantMessage(blocks...)
}

// sources lists the pages a reply cited, each once, in the order first cited.
// Taken from the citations and not from the search results: a page that was
// found and not used is not a source.
func sources(content []sdk.ContentBlockUnion) []ai.Source {
	var out []ai.Source
	seen := map[string]bool{}
	for _, block := range content {
		text, ok := block.AsAny().(sdk.TextBlock)
		if !ok {
			continue
		}
		for _, citation := range text.Citations {
			c, ok := citation.AsAny().(sdk.CitationsWebSearchResultLocation)
			if !ok || c.URL == "" || seen[c.URL] {
				continue
			}
			seen[c.URL] = true
			out = append(out, ai.Source{Title: c.Title, URL: c.URL})
		}
	}
	return out
}

// searched reports whether a block belongs to a web search, or quotes one.
func searched(block sdk.ContentBlockUnion) bool {
	switch b := block.AsAny().(type) {
	case sdk.ServerToolUseBlock, sdk.WebSearchToolResultBlock:
		return true
	case sdk.TextBlock:
		return len(b.Citations) > 0
	}
	return false
}
