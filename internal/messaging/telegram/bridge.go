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

// leaveTimeout bounds declining a group. Two API calls and no model, so it
// needs nothing like the budget an answer does.
const leaveTimeout = 30 * time.Second

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

	// typingEvery is how often the indicator is refreshed. Zero means
	// typingInterval; a test sets it small so it does not have to wait out a
	// real one.
	typingEvery time.Duration
}

func (b *bridge) typingTick() time.Duration {
	if b.typingEvery > 0 {
		return b.typingEvery
	}
	return typingInterval
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
	in, callbackID, what := u.inbound()

	switch what {
	case ignoreUpdate:
		// A sticker, someone joining. Not an error and not a question.
		return

	case leaveChat:
		detached, cancel := context.WithTimeout(context.WithoutCancel(ctx), leaveTimeout)
		go func() {
			defer cancel()
			b.declineGroup(detached, in.ExternalID)
		}()
		return
	}

	detached, cancel := context.WithTimeout(context.WithoutCancel(ctx), replyTimeout)
	go func() {
		defer cancel()
		b.answer(detached, in, callbackID)
	}()
}

// declineGroup explains itself once, then removes the bot from the chat.
//
// Silence would be worse. Somebody added this bot on purpose, and one that
// simply never answers looks broken rather than deliberate. Leaving is the part
// that matters though: a chat North stays in is a chat whose id could later be
// linked, and a group's id is shared by everybody in it.
func (b *bridge) declineGroup(ctx context.Context, externalID string) {
	b.log.Warn("telegram declined a group chat", "chat", externalID)

	notice := messaging.OutboundMessage{
		Text: "I only work in a direct message — each person's coaching is their own, " +
			"and I cannot tell who is who in a group. Message me privately instead.",
	}
	if err := b.client.Send(ctx, externalID, notice); err != nil {
		// Worth a line but not worth stopping for: leaving is the part that
		// closes the hole, and it should happen whether or not the note landed.
		b.log.Warn("telegram could not explain itself before leaving", "error", err)
	}

	if err := b.client.Leave(ctx, externalID); err != nil {
		b.log.Error("telegram could not leave a group chat", "error", err, "chat", externalID)
	}
}

// typingInterval is how often the indicator is refreshed.
//
// Telegram clears it after about five seconds, so anything at or above that
// leaves visible gaps. Four is under the limit with room for a slow request.
const typingInterval = 4 * time.Second

// keepTyping shows "typing…" until the returned function is called.
//
// One refresh is not enough: a coach turn takes tens of seconds and Telegram
// drops the indicator after five, so a single call leaves the person watching
// nothing happen and concluding the bot is broken. That is the moment people
// stop using it, which makes this worth a goroutine.
//
// Failures are logged once rather than every tick. A bot that cannot show an
// indicator can still answer, and a log line every four seconds for the length
// of a stuck turn would bury the reason it was stuck.
func (b *bridge) keepTyping(ctx context.Context, externalID string) (stop func()) {
	typing, cancel := context.WithCancel(ctx)

	go func() {
		var failed bool
		send := func() {
			if err := b.client.Typing(typing, externalID); err != nil && !failed {
				failed = true
				b.log.Warn("telegram could not send a typing indicator", "error", err)
			}
		}

		send()

		ticker := time.NewTicker(b.typingTick())
		defer ticker.Stop()
		for {
			select {
			case <-typing.Done():
				return
			case <-ticker.C:
				send()
			}
		}
	}()

	return cancel
}

func (b *bridge) answer(ctx context.Context, in messaging.InboundMessage, callbackID string) {
	// First, so the tapped button stops spinning while the coach thinks.
	if callbackID != "" {
		if err := b.client.AnswerCallback(ctx, callbackID); err != nil {
			b.log.Warn("telegram could not answer a callback query", "error", err)
		}
	}

	if err := b.fillAttachment(ctx, &in); err != nil {
		b.log.Warn("telegram could not download a photo", "error", err)
		_ = b.client.Send(ctx, in.ExternalID, messaging.OutboundMessage{
			Text: "I could not download that photo. Try sending it again?",
		})
		return
	}

	stopTyping := b.keepTyping(ctx, in.ExternalID)

	out, err := b.messages.Handle(ctx, in)
	stopTyping()
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

func (b *bridge) fillAttachment(ctx context.Context, in *messaging.InboundMessage) error {
	if in.Attachment == nil || in.Attachment.FileID == "" || len(in.Attachment.Bytes) > 0 {
		return nil
	}
	data, mime, err := b.client.File(ctx, in.Attachment.FileID)
	if err != nil {
		return err
	}
	in.Attachment.Bytes = data
	if in.Attachment.MIMEType == "" {
		in.Attachment.MIMEType = mime
	}
	return nil
}
