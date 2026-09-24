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
		// The thinking that led to a call has to come back ahead of it,
		// unchanged, or the API refuses the continuation.
		blocks = append(thinkingBlocks(m.ProviderState), blocks...)
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

// lostThinking reports a tool call in the turn in progress replayed without
// the thinking that led to it, which happens when a turn is rebuilt from the
// database after an approval.
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
