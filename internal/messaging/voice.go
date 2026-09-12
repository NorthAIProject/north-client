package messaging

import (
	"context"
	"strings"

	"github.com/NorthAIProject/north-client/internal/quota"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/spend"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/voice"
)

// Voice turns a recording into the sentence the person would have typed.
//
// Declared here rather than imported as a concrete type for the reason every
// other dependency in this package is: what this needs is one method, and a
// nil one is a deployment that refuses voice notes in words instead of dropping
// them.
type Voice interface {
	Transcribe(ctx context.Context, user users.User, audio []byte, surface string) (string, error)
}

// transcribeVoice replaces a voice note with the words in it.
//
// This is the whole feature, and it lives here rather than in the Telegram
// adapter on purpose. The adapter's job is to translate one platform's shapes
// into this package's; deciding what a recording costs, what is too long, and
// what to say when it cannot be heard is product behaviour, and putting it in
// the adapter would mean the next platform copied it.
//
// It rewrites in place and clears the attachment, so everything downstream —
// incomingFrom, the coach, the stored history — sees an ordinary typed turn.
// That is the point: the answer to a spoken sentence should be the answer to
// the sentence.
//
// The three returns are the three outcomes: keep going, say this and stop, or
// a real failure. A transcription that fails is the middle one, never the
// third, because an error returned from here reaches the bridge as the generic
// apology — true, and useless to somebody who just spoke into their phone.
func (s *Service) transcribeVoice(ctx context.Context, user users.User, in *InboundMessage) (OutboundMessage, bool, error) {
	if in.Attachment == nil || in.Attachment.Kind != KindVoice {
		return OutboundMessage{}, true, nil
	}

	if s.voice == nil {
		return OutboundMessage{Text: i18n.T(ctx, "tg.voice.unavailable")}, false, nil
	}

	// Bounds first, and both of them before the quota: a recording refused for
	// its length must not also cost the person a dictation. The duration is the
	// platform's claim, which is why the bytes are checked too.
	if seconds := in.Attachment.DurationSeconds; seconds > voice.MaxSeconds {
		return OutboundMessage{Text: i18n.T(ctx, "tg.voice.toolong")}, false, nil
	}
	if len(in.Attachment.Bytes) == 0 {
		return OutboundMessage{Text: i18n.T(ctx, "tg.voice.silent")}, false, nil
	}
	if len(in.Attachment.Bytes) > voice.MaxBytes {
		return OutboundMessage{Text: i18n.T(ctx, "tg.voice.toobig")}, false, nil
	}

	// Metered against the same budget the web recorder spends. It is the same
	// act — a recording becoming text through a paid model — and a per-person
	// allowance should not double because they opened a different app.
	if s.quotas != nil {
		decision, err := s.quotas.Consume(ctx, user.ID, string(user.Tier), quota.VoiceCapture)
		if err != nil {
			// Consume fails open by design; an error here is a counter
			// problem, not a person's problem.
			s.log.Warn("messaging could not check the voice quota", "error", err, "user_id", user.ID)
		} else if !decision.Allowed {
			s.log.Warn("messaging voice note refused by quota", "user_id", user.ID)
			return OutboundMessage{Text: quotaMessage(ctx, decision)}, false, nil
		}
	}

	text, err := s.voice.Transcribe(ctx, user, in.Attachment.Bytes, spend.SurfaceTelegramVoice)
	if err != nil {
		// Logged with the cause, answered without it. What went wrong is an
		// operator's business; what the person needs is to know what to do next.
		s.log.Warn("messaging could not transcribe a voice note", "error", err, "user_id", user.ID)

		// Two different situations, and telling them apart is the difference
		// between useful advice and a shrug. Unavailable means nothing on this
		// deployment can listen — no endpoint configured, or one refusing this
		// deployment — and no amount of retrying will change that, so say what
		// will work instead. Anything else, including a busy recogniser, is a
		// failure worth a second attempt.
		if apperr.Is(err, apperr.ErrUnavailable) {
			return OutboundMessage{Text: i18n.T(ctx, "tg.voice.unavailable")}, false, nil
		}
		return OutboundMessage{Text: i18n.T(ctx, "tg.voice.failed")}, false, nil
	}
	// Trimmed here rather than trusted from the other side of an interface.
	// The production implementation already does it, but "empty" is what this
	// branch turns on, and a turn of whitespace reaching the coach is refused
	// several layers down with a message about typing something — which is not
	// a sensible thing to say to somebody who just spoke.
	text = strings.TrimSpace(text)
	if text == "" {
		// Silence, a pocket, a tap. Asking again costs nothing; handing the
		// coach an empty turn would make it invent a subject.
		return OutboundMessage{Text: i18n.T(ctx, "tg.voice.silent")}, false, nil
	}

	in.Text = text
	in.Attachment = nil
	return OutboundMessage{}, true, nil
}
