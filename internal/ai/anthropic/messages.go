package anthropic

import (
	"encoding/base64"
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

// readableImage is every image type the Messages API accepts. Anything else,
// such as an iPhone's HEIC, is named in text rather than sent: an image block
// it cannot read fails the whole request.
var readableImage = map[string]bool{
	"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true,
}

func contentBlocks(m ai.Message) []sdk.ContentBlockParamUnion {
	var blocks []sdk.ContentBlockParamUnion
	for _, part := range m.Parts {
		switch {
		case part.Text != "":
			blocks = append(blocks, sdk.NewTextBlock(part.Text))
		case len(part.InlineData) > 0 && readableImage[part.MIMEType]:
			blocks = append(blocks, sdk.NewImageBlockBase64(part.MIMEType,
				base64.StdEncoding.EncodeToString(part.InlineData)))
		case len(part.InlineData) > 0 || part.FileURI != "":
			// Claude cannot take this kind of file inline. Naming it keeps
			// the turn honest without failing the whole request.
			blocks = append(blocks, sdk.NewTextBlock("[attachment: "+part.MIMEType+" not shown]"))
		}
	}
	if len(m.ToolCalls) > 0 {
		// The turn that made these calls has to come back as the model wrote
		// it, thinking and all, or the API refuses the continuation.
		if replayed, ok := replayTurn(m); ok {
			return append(blocks, replayed...)
		}
	}
	for _, call := range m.ToolCalls {
		blocks = append(blocks, toolUse(call))
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

// stateBlock is one block of an assistant turn that made tool calls, as the
// API needs it back: thinking, redacted thinking and text in full, a tool call
// by its id alone (the call itself travels in ai.Message.ToolCalls).
// Serialised into ProviderState and nowhere else.
type stateBlock struct {
	Kind      string `json:"kind"`
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	Data      string `json:"data,omitempty"`
	Text      string `json:"text,omitempty"`
	ToolUseID string `json:"tool_use_id,omitempty"`
}

const (
	kindThinking = "thinking"
	kindRedacted = "redacted_thinking"
	kindText     = "text"
	kindToolUse  = "tool_use"
)

// turnState records a tool-calling turn's blocks in the order the model wrote
// them. The API refuses a continuation whose thinking was edited, reordered or
// partly dropped, so the whole turn is kept rather than a projection of it.
// Kept even when the model did not think, so that a replay can tell "recorded,
// nothing to add" from "lost" (see lostThinking).
func turnState(content []sdk.ContentBlockUnion) json.RawMessage {
	var kept []stateBlock
	for _, block := range content {
		switch b := block.AsAny().(type) {
		case sdk.ThinkingBlock:
			kept = append(kept, stateBlock{Kind: kindThinking, Thinking: b.Thinking, Signature: b.Signature})
		case sdk.RedactedThinkingBlock:
			kept = append(kept, stateBlock{Kind: kindRedacted, Data: b.Data})
		case sdk.TextBlock:
			kept = append(kept, stateBlock{Kind: kindText, Text: b.Text})
		case sdk.ToolUseBlock:
			kept = append(kept, stateBlock{Kind: kindToolUse, ToolUseID: b.ID})
		}
	}
	if len(kept) == 0 {
		return nil
	}
	raw, _ := json.Marshal(kept)
	return raw
}

// replayTurn rebuilds a tool-calling turn from its state, in the original
// order, taking each call from ToolCalls by id. False when there is no usable
// state, and the caller builds the turn from ToolCalls alone.
func replayTurn(m ai.Message) ([]sdk.ContentBlockParamUnion, bool) {
	var kept []stateBlock
	if len(m.ProviderState) == 0 || json.Unmarshal(m.ProviderState, &kept) != nil || len(kept) == 0 {
		return nil, false
	}

	calls := make(map[string]ai.ToolCall, len(m.ToolCalls))
	for _, call := range m.ToolCalls {
		calls[call.ID] = call
	}

	out := make([]sdk.ContentBlockParamUnion, 0, len(kept))
	for _, k := range kept {
		switch k.Kind {
		case kindThinking:
			out = append(out, sdk.NewThinkingBlock(k.Signature, k.Thinking))
		case kindRedacted:
			out = append(out, sdk.NewRedactedThinkingBlock(k.Data))
		case kindText:
			if k.Text != "" {
				out = append(out, sdk.NewTextBlock(k.Text))
			}
		case kindToolUse:
			if call, ok := calls[k.ToolUseID]; ok {
				out = append(out, toolUse(call))
				delete(calls, k.ToolUseID)
			}
		}
	}
	// A call the state does not mention still has to be sent, or its result
	// would answer nothing. In ToolCalls order, after the recorded blocks.
	for _, call := range m.ToolCalls {
		if _, left := calls[call.ID]; left {
			out = append(out, toolUse(call))
		}
	}
	return out, true
}

func toolUse(call ai.ToolCall) sdk.ContentBlockParamUnion {
	var input any = map[string]any{}
	if len(call.Arguments) > 0 {
		_ = json.Unmarshal(call.Arguments, &input)
	}
	return sdk.NewToolUseBlock(call.ID, input, call.Name)
}

// lostThinking reports a tool call in the turn in progress replayed with no
// record of the turn that made it: a row stored before provider_state existed,
// or a call made by another provider earlier in the same turn.
//
// Only the current turn counts: the API requires thinking back within a
// tool-use turn and allows it to be omitted from earlier ones, so a tool round
// from yesterday, stored without state, must not switch thinking off today.
// The current turn is everything after the person's last own message.
func lostThinking(in []ai.Message) bool {
	for i := len(in) - 1; i >= 0; i-- {
		m := in[i]
		if m.Role == ai.RoleUser && len(m.ToolResults) == 0 {
			return false
		}
		if len(m.ToolCalls) > 0 && len(m.ProviderState) == 0 {
			return true
		}
	}
	return false
}
