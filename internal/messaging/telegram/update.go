// Package telegram is North's Telegram adapter.
//
// It owns everything Telegram-shaped — the Bot API's JSON, its webhook secret
// header, its inline keyboards — and nothing else. Every message it receives
// leaves this package as a messaging.InboundMessage and every reply arrives
// back as a messaging.OutboundMessage, so no business logic can accumulate
// here and no other package has to know Telegram exists.
package telegram

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/NorthAIProject/north-client/internal/messaging"
)

// update is the slice of Telegram's Update object North reads.
//
// Deliberately partial. The full object carries edited messages, channel
// posts, polls, shipping queries and a dozen other things; decoding only what
// is used means an unfamiliar update is ignored rather than mis-handled.
type update struct {
	UpdateID int64 `json:"update_id"`

	Message *struct {
		Chat struct {
			ID int64 `json:"id"`
		} `json:"chat"`
		Text string `json:"text"`
		Date int64  `json:"date"`
	} `json:"message"`

	// CallbackQuery is a tapped inline-keyboard button. Its Data is the
	// Option.Value North put on the button, so a tap and the typed word arrive
	// as the same InboundMessage.Text and the adapter needs no second path.
	CallbackQuery *struct {
		ID      string `json:"id"`
		Data    string `json:"data"`
		Message *struct {
			Chat struct {
				ID int64 `json:"id"`
			} `json:"chat"`
		} `json:"message"`
	} `json:"callback_query"`
}

// decodeUpdate parses one Telegram update. The bool reports whether it parsed
// at all; whether it carries anything worth answering is inbound's question.
func decodeUpdate(raw []byte) (update, bool) {
	var u update
	if err := json.Unmarshal(raw, &u); err != nil {
		return update{}, false
	}
	return u, true
}

// inbound returns the message, the callback query id that must be answered
// (empty for an ordinary message), and whether there was anything to act on.
//
// An update carrying no text — a photo, a sticker, a member joining — is not
// an error, it is simply not a question, and answering it would be worse than
// ignoring it.
func (u update) inbound() (messaging.InboundMessage, string, bool) {
	switch {
	case u.CallbackQuery != nil && u.CallbackQuery.Message != nil:
		return messaging.InboundMessage{
			Platform:   messaging.PlatformTelegram,
			ExternalID: chatID(u.CallbackQuery.Message.Chat.ID),
			Text:       u.CallbackQuery.Data,
			UpdateID:   u.UpdateID,
			ReceivedAt: time.Now(),
		}, u.CallbackQuery.ID, true

	case u.Message != nil && u.Message.Text != "":
		received := time.Now()
		if u.Message.Date > 0 {
			received = time.Unix(u.Message.Date, 0)
		}
		return messaging.InboundMessage{
			Platform:   messaging.PlatformTelegram,
			ExternalID: chatID(u.Message.Chat.ID),
			Text:       u.Message.Text,
			UpdateID:   u.UpdateID,
			ReceivedAt: received,
		}, "", true

	default:
		return messaging.InboundMessage{}, "", false
	}
}

// chatID renders Telegram's numeric chat id as the text external id the
// messaging package stores. Text because the next platform's is a string.
func chatID(id int64) string {
	return strconv.FormatInt(id, 10)
}
