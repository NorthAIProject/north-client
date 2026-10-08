package anthropic

import (
	"context"
	"encoding/json"

	"github.com/FACorreiaa/go-utils/pkg/util"
	sdk "github.com/anthropics/anthropic-sdk-go"

	"github.com/NorthAIProject/north-client/internal/ai"
)

// Chat streams a reply: text as it arrives, then any tool calls in one chunk
// (a half-built argument object is of no use to the coach), then usage.
//
// The opening of the stream is read before Chat returns. Until the reply has
// produced text or started a tool call, nothing has reached the person, so an
// HTTP error, a refusal or an empty reply is still returned as Chat's own
// error and the runner can ask the next provider. Thinking comes first and is
// read in that window too; it is never shown, so holding it back costs nothing.
// Once the reply has committed, failures arrive on the channel as before.
func (c *Client) Chat(ctx context.Context, req ai.Request) (<-chan ai.StreamChunk, error) {
	stream := c.sdk.Messages.NewStreaming(ctx, c.params(req))

	var msg sdk.Message
	var held []sdk.MessageStreamEventUnion
	ended := true
	for stream.Next() {
		event := stream.Current()
		if err := msg.Accumulate(event); err != nil {
			_ = stream.Close()
			return nil, c.classify(err)
		}
		held = append(held, event)
		if commits(event) {
			ended = false
			break
		}
	}
	if ended {
		if err := stream.Err(); err != nil {
			_ = stream.Close()
			return nil, c.classify(err)
		}
		if err := stopError(msg.StopReason, msg.Content); err != nil {
			_ = stream.Close()
			return nil, err
		}
	}

	out := make(chan ai.StreamChunk)

	go func() {
		defer close(out)
		defer func() { _ = stream.Close() }()

		// Replayed for their text only: msg has already accumulated them.
		for _, event := range held {
			if !sendText(ctx, out, event) {
				return
			}
		}

		if !ended {
			for stream.Next() {
				event := stream.Current()
				if err := msg.Accumulate(event); err != nil {
					send(ctx, out, ai.StreamChunk{Err: c.classify(err)})
					return
				}
				if !sendText(ctx, out, event) {
					return
				}
			}
			if err := stream.Err(); err != nil {
				send(ctx, out, ai.StreamChunk{Err: c.classify(err)})
				return
			}
			if err := stopError(msg.StopReason, msg.Content); err != nil {
				send(ctx, out, ai.StreamChunk{Err: err})
				return
			}
		}

		if calls := toolCalls(msg.Content); len(calls) > 0 {
			if !send(ctx, out, ai.StreamChunk{ToolCalls: calls, ProviderState: turnState(msg.Content)}) {
				return
			}
		}
		send(ctx, out, ai.StreamChunk{Usage: util.Ptr(usage(msg.Usage)), Model: string(msg.Model)})
	}()

	return out, nil
}

// commits reports whether an event is the reply's first visible output: a
// piece of text, or the start of a tool call. Past it, an error can no longer
// be handed to another provider without the person seeing two answers.
func commits(event sdk.MessageStreamEventUnion) bool {
	switch e := event.AsAny().(type) {
	case sdk.ContentBlockDeltaEvent:
		text, ok := e.Delta.AsAny().(sdk.TextDelta)
		return ok && text.Text != ""
	case sdk.ContentBlockStartEvent:
		return e.ContentBlock.Type == "tool_use"
	}
	return false
}

// sendText forwards an event's text, if it carries any. False means stop.
func sendText(ctx context.Context, out chan<- ai.StreamChunk, event sdk.MessageStreamEventUnion) bool {
	delta, ok := event.AsAny().(sdk.ContentBlockDeltaEvent)
	if !ok {
		return true
	}
	text, ok := delta.Delta.AsAny().(sdk.TextDelta)
	if !ok || text.Text == "" {
		return true
	}
	return send(ctx, out, ai.StreamChunk{Text: text.Text})
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
