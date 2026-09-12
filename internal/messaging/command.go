package messaging

import (
	"context"
	"strings"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Commands a linked person can send.
//
// Handled here rather than in the Telegram package because none of them is a
// Telegram idea: Discord and WhatsApp use the same leading slash, and "help" is
// not a platform concept. What is platform-specific is registering the menu so
// the client offers them, and that stays in the adapter.
const (
	commandStart  = "/start"
	commandHelp   = "/help"
	commandStats  = "/stats"
	commandUnlink = "/unlink"
)

// Commands lists what a client's command menu should offer.
//
// Exported so an adapter can register it — Telegram's setMyCommands wants
// exactly this, and hard-coding the list there would let the two drift.
func Commands() []Command {
	return []Command{
		{Name: strings.TrimPrefix(commandHelp, "/"), Description: "what I can do"},
		{Name: strings.TrimPrefix(commandStats, "/"), Description: "how things are going — add week, month or year"},
		{Name: strings.TrimPrefix(commandUnlink, "/"), Description: "disconnect this chat from your account"},
	}
}

// Command is one entry in a client's command menu.
type Command struct {
	Name        string
	Description string
}

// parseCommand splits a leading slash command from its arguments.
//
// Telegram appends the bot's username to commands sent in a chat with more than
// one bot — "/help@north_coach_bot" — so that suffix is stripped. It costs one
// line and its absence would look like the command simply not working.
func parseCommand(text string) (name, args string, ok bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", "", false
	}

	name, args, _ = strings.Cut(text, " ")
	if at := strings.IndexByte(name, '@'); at > 0 {
		name = name[:at]
	}
	return strings.ToLower(name), strings.TrimSpace(args), true
}

// runCommand answers a command, and reports whether it was one.
//
// Called before the pending-approval check and before the quota is spent: a
// command is not a coach turn, so it must neither cost a message nor be
// mistaken for an answer to a waiting confirmation. Asking for help in the
// middle of being asked to confirm a write leaves that write exactly where it
// was.
//
// An unrecognised command is deliberately not an error. Somebody typing
// "/summarise my week" means it as a sentence, and the useful thing to do with
// a sentence is answer it — so anything unknown falls through to the coach.
func (s *Service) runCommand(ctx context.Context, user users.User, in InboundMessage) (OutboundMessage, bool, error) {
	name, args, ok := parseCommand(in.Text)
	if !ok {
		return OutboundMessage{}, false, nil
	}

	switch name {
	case commandStart:
		return OutboundMessage{Text: i18n.Tf(ctx, "tg.start", user.Email)}, true, nil

	case commandHelp:
		return OutboundMessage{Text: i18n.T(ctx, "tg.help")}, true, nil

	case commandStats:
		return s.statsReply(ctx, user, args), true, nil

	case commandUnlink:
		unlinked, err := s.Unlink(ctx, user.ID, in.Platform)
		if err != nil {
			return OutboundMessage{}, true, err
		}
		if !unlinked {
			return OutboundMessage{Text: i18n.T(ctx, "tg.notlinked")}, true, nil
		}

		s.log.Info("messaging unlinked a chat", "platform", in.Platform, "user_id", user.ID)
		return OutboundMessage{Text: i18n.T(ctx, "tg.unlinked")}, true, nil

	default:
		return OutboundMessage{}, false, nil
	}
}

// The help text lives in the message catalogue as "tg.help", with the same
// reasoning it always had: it is deliberately about what Khepri does rather
// than what it is, and the two things worth saying are the ones somebody
// cannot discover by trying — that writes are confirmed before they happen,
// and that this is the same conversation as the web app rather than a second
// one.

// statsReply answers /stats for whichever window the argument names.
//
// The argument is handed straight to the range parser, which resolves anything
// it does not recognise to today. That is deliberate: somebody typing
// "/stats lsat week" should get a number, not a lecture about spelling, and
// the parser already takes the same position for a hand-edited URL.
//
// Errors are answered rather than returned. A failure to read the numbers is
// not a failure to handle the command, and returning it here would surface a
// database error to somebody who asked how their week went.
func (s *Service) statsReply(ctx context.Context, user users.User, args string) OutboundMessage {
	if s.stats == nil {
		return OutboundMessage{Text: i18n.T(ctx, "tg.stats.unavailable")}
	}

	rg := timerange.Parse(strings.ToLower(args), user.Location())

	text, photo, err := s.stats.Digest(ctx, user, rg)
	if err != nil {
		s.log.Error("messaging stats digest failed", "user_id", user.ID, "error", err)
		return OutboundMessage{Text: i18n.T(ctx, "tg.stats.failed")}
	}
	if strings.TrimSpace(text) == "" {
		return OutboundMessage{Text: i18n.T(ctx, "tg.stats.empty")}
	}

	out := OutboundMessage{Text: text, Photo: photo}
	if len(photo) > 0 {
		out.PhotoCaption = rg.Label
	}
	return out
}
