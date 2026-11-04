package anthropic

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"

	"github.com/NorthAIProject/north-client/internal/ai"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/lifecycle"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
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
//
// A managed client's opening is also on a budget. A provider that takes the
// request and says nothing would otherwise spend the caller's whole deadline,
// and that deadline passing does not fail over, so the next provider would never
// be asked. The budget ends with the first visible output: past that, slowness
// is the reply being written and there is nobody left to hand it to.
func (c *Client) Chat(ctx context.Context, req ai.Request) (<-chan ai.StreamChunk, error) {
	p := c.params(req)

	attemptCtx, cancel := context.WithCancel(ctx)
	withinBudget := startBudget(c.firstTokenBudget, cancel)

	stream := c.sdk.Messages.NewStreaming(attemptCtx, p, requestOptions(p)...)
	msg, held, ended, err := c.open(stream)

	// Checked against ctx, not attemptCtx: the budget cancelled that one itself.
	// A fresh error rather than a wrapped one, because the cancellation it would
	// wrap is what Failover reads as the caller having gone.
	if !withinBudget() && ctx.Err() == nil {
		err = fmt.Errorf("%w: anthropic: no reply within %s", apperr.ErrUnavailable, c.firstTokenBudget)
	}
	if err != nil {
		_ = stream.Close()
		cancel()
		return nil, err
	}

	out := make(chan ai.StreamChunk)

	go func() {
		// Deferred in this order so a panic still closes out: Recover stops it
		// and close runs after.
		defer close(out)
		defer lifecycle.Recover(middleware.FromContext(ctx), "anthropic stream")
		defer cancel()
		defer func() { _ = stream.Close() }()

		err := c.relay(ctx, out, stream, &msg, held, ended)

		// Sent whatever became of ctx. A send that also selected on it would
		// drop both exactly when ctx had expired, and the reply would then read
		// as finished with nothing spent. A reader drains to close, so these
		// cannot block for ever. Usage goes first because the error is the last
		// chunk a reader takes.
		if spent := usage(msg.Usage); err == nil || spent != (ai.Usage{}) {
			out <- ai.StreamChunk{Usage: &spent, Model: string(msg.Model)}
		}
		if err != nil {
			out <- ai.StreamChunk{Err: err}
		}
	}()

	return out, nil
}

// startBudget calls expire after d, unless the returned function is called
// first. That function reports whether it was, and is called once. A d of zero
// is no budget at all.
func startBudget(d time.Duration, expire func()) (withinBudget func() bool) {
	if d <= 0 {
		return func() bool { return true }
	}
	return time.AfterFunc(d, expire).Stop
}

// open reads the stream up to the reply's first visible output and returns what
// it read. ended means the stream finished in that window.
func (c *Client) open(stream *ssestream.Stream[sdk.MessageStreamEventUnion]) (msg sdk.Message, held []sdk.MessageStreamEventUnion, ended bool, err error) {
	for stream.Next() {
		event := stream.Current()
		if err := msg.Accumulate(event); err != nil {
			return msg, nil, false, c.classify(err)
		}
		held = append(held, event)
		if commits(event) {
			return msg, held, false, nil
		}
	}
	if err := stream.Err(); err != nil {
		return msg, nil, false, c.classify(err)
	}
	if err := stopError(msg.StopReason, msg.Content); err != nil {
		return msg, nil, false, err
	}
	return msg, held, true, nil
}

// relay sends the reply to out as it arrives, accumulating it into msg, and
// returns what ended it. nil means the reply is complete.
func (c *Client) relay(
	ctx context.Context,
	out chan<- ai.StreamChunk,
	stream *ssestream.Stream[sdk.MessageStreamEventUnion],
	msg *sdk.Message,
	held []sdk.MessageStreamEventUnion,
	ended bool,
) error {
	// Replayed for their text only: msg has already accumulated them.
	for _, event := range held {
		if !sendText(ctx, out, event) {
			return c.classify(ctx.Err())
		}
	}

	if !ended {
		for stream.Next() {
			event := stream.Current()
			if err := msg.Accumulate(event); err != nil {
				return c.classify(err)
			}
			if !sendText(ctx, out, event) {
				return c.classify(ctx.Err())
			}
		}
		if err := stream.Err(); err != nil {
			return c.classify(err)
		}
		if err := stopError(msg.StopReason, msg.Content); err != nil {
			return err
		}
	}

	if calls := toolCalls(msg.Content); len(calls) > 0 {
		if !send(ctx, out, ai.StreamChunk{ToolCalls: calls, ProviderState: turnState(msg.Content)}) {
			return c.classify(ctx.Err())
		}
	}
	return nil
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

// send delivers a chunk unless the caller has gone away. False means stop, and
// that ctx is done.
func send(ctx context.Context, out chan<- ai.StreamChunk, chunk ai.StreamChunk) bool {
	select {
	case out <- chunk:
		return true
	case <-ctx.Done():
		return false
	}
}
