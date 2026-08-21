package messaging

import (
	"context"
	"fmt"
	"strings"

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
	commandUnlink = "/unlink"
)

// Commands lists what a client's command menu should offer.
//
// Exported so an adapter can register it — Telegram's setMyCommands wants
// exactly this, and hard-coding the list there would let the two drift.
func Commands() []Command {
	return []Command{
		{Name: strings.TrimPrefix(commandHelp, "/"), Description: "what I can do"},
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
	name, _, ok := parseCommand(in.Text)
	if !ok {
		return OutboundMessage{}, false, nil
	}

	switch name {
	case commandStart:
		return OutboundMessage{Text: fmt.Sprintf(
			"You are linked to %s. Ask me anything you would ask in the web app — "+
				"I have the same memory, goals and check-ins.\n\nSend /help to see what I can do.",
			user.Email)}, true, nil

	case commandHelp:
		return OutboundMessage{Text: helpText}, true, nil

	case commandUnlink:
		unlinked, err := s.Unlink(ctx, user.ID, in.Platform)
		if err != nil {
			return OutboundMessage{}, true, err
		}
		if !unlinked {
			return OutboundMessage{Text: "This chat is not linked to a Khepri account."}, true, nil
		}

		s.log.Info("messaging unlinked a chat", "platform", in.Platform, "user_id", user.ID)
		return OutboundMessage{Text: "Disconnected. Nothing you have said is deleted — the whole " +
			"conversation is still in the web app. To connect again, get a new code from " +
			"Settings → Agent connections."}, true, nil

	default:
		return OutboundMessage{}, false, nil
	}
}

// helpText is deliberately about what Khepri does rather than what it is.
//
// The two things worth saying are the ones somebody cannot discover by trying:
// that writes are confirmed before they happen, and that this is the same
// conversation as the web app rather than a second one.
const helpText = "Ask me anything you would ask in the web app — how a goal is going, " +
	"what your week looked like, whether to train today.\n\n" +
	"This is the same conversation as the web chat. Ask here, and the answer is there too.\n\n" +
	"Before I write anything down — a check-in, a goal — I will show you what I am about to " +
	"do and wait for a yes.\n\n" +
	"/help — this message\n" +
	"/unlink — disconnect this chat from your account"
