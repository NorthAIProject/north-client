package anthropic

import (
	"encoding/base64"
	"encoding/json"
	"strings"

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
