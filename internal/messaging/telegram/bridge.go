package telegram

import (
	"context"
	"log/slog"
	"time"

	"github.com/NorthAIProject/north-client/internal/messaging"
)

// replyTimeout bounds one detached turn.
//
// Above the coach's own five-minute generation timeout, so this never fires
// first and turns a slow-but-working answer into a lost one; below anything
// that would keep a goroutine alive indefinitely.
const replyTimeout = 6 * time.Minute

// Handler is the messaging behaviour a bridge needs. An interface so the two
// edges can be tested without a database behind them.
type Handler interface {
	Handle(ctx context.Context, in messaging.InboundMessage) (messaging.OutboundMessage, error)
}

// bridge is the part the webhook and the poller share: one update in, one
// reply sent. Both edges differ only in how an update reaches them.
type bridge struct {
	messages Handler
	client   *Client
	log      *slog.Logger
}

// dispatchRaw decodes a webhook body and answers it.
func (b *bridge) dispatchRaw(ctx context.Context, raw []byte) {
	u, ok := decodeUpdate(raw)
	if !ok {
		return
	}
	b.dispatch(ctx, u)
}

// dispatch answers one update on a context detached from whatever delivered it.
//
// Detached because a coach turn can take minutes and both edges need to move
// on immediately: the webhook has to acknowledge before Telegram retries, and
// the poller has to keep polling. This is the same trade the coach itself
// makes in SendMessage, and it fails the same way — a restart mid-generation
// loses the outbound push while the reply is still persisted and still shows
// up in the web thread. What is lost is the notification, not the answer.
func (b *bridge) dispatch(ctx context.Context, u update) {
	in, callbackID, ok := u.inbound()
	if !ok {
		// A sticker, a photo, someone joining. Not an error and not a
		// question, so there is nothing to answer.
		return
	}

	detached, cancel := context.WithTimeout(context.WithoutCancel(ctx), replyTimeout)
	go func() {
		defer cancel()
		b.answer(detached, in, callbackID)
	}()
}

func (b *bridge) answer(ctx context.Context, in messaging.InboundMessage, callbackID string) {
	// First, so the tapped button stops spinning while the coach thinks.
	if callbackID != "" {
		if err := b.client.AnswerCallback(ctx, callbackID); err != nil {
			b.log.Warn("telegram could not answer a callback query", "error", err)
		}
	}

	if err := b.client.Typing(ctx, in.ExternalID); err != nil {
		b.log.Warn("telegram could not send a typing indicator", "error", err)
	}

	out, err := b.messages.Handle(ctx, in)
	if err != nil {
		b.log.Error("telegram could not answer a message", "error", err, "platform", in.Platform)
		// Say something rather than nothing. A person who gets silence assumes
		// the bot is broken and stops using it; one who gets an apology tries
		// again, and the failure is in the logs either way.
		out = messaging.OutboundMessage{Text: "Something went wrong on my side. Try again in a moment."}
	}

	if err := b.client.Send(ctx, in.ExternalID, out); err != nil {
		b.log.Error("telegram could not deliver a reply", "error", err)
	}
}
