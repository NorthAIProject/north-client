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
			if !send(ctx, out, ai.StreamChunk{ToolCalls: calls, ProviderState: turnState(msg.Content)}) {
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
