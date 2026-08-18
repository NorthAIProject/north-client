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
		Chat     chat      `json:"chat"`
		Text     string    `json:"text"`
		Caption  string    `json:"caption"`
		Date     int64     `json:"date"`
		Photo    []tgPhoto `json:"photo"`
		Document *tgDoc    `json:"document"`
	} `json:"message"`

	// CallbackQuery is a tapped inline-keyboard button. Its Data is the
	// Option.Value North put on the button, so a tap and the typed word arrive
	// as the same InboundMessage.Text and the adapter needs no second path.
	CallbackQuery *struct {
		ID      string `json:"id"`
		Data    string `json:"data"`
		Message *struct {
			Chat chat `json:"chat"`
		} `json:"message"`
	} `json:"callback_query"`
}

// tgPhoto is one size of a Telegram photo.
type tgPhoto struct {
	FileID string `json:"file_id"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// tgDoc is a file sent as a document.
type tgDoc struct {
	FileID   string `json:"file_id"`
	FileName string `json:"file_name"`
	MIMEType string `json:"mime_type"`
}

// chat is where a message came from.
//
// Type is the field this whole guard turns on. Telegram sends "private",
// "group", "supergroup" or "channel", and only the first is one person.
type chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

const (
	chatPrivate    = "private"
	chatGroup      = "group"
	chatSupergroup = "supergroup"
	chatChannel    = "channel"
)

// intent is what an update asks the adapter to do.
type intent int

const (
	// ignoreUpdate covers everything that is not a question: a sticker,
	// someone joining, and any chat type North does not recognise. Photos
	// are questions now.
	ignoreUpdate intent = iota

	// answerUpdate is a message from one person, in a private chat.
	answerUpdate

	// leaveChat is a group, supergroup or channel.
	//
	// North refuses these and removes itself rather than merely staying quiet.
	// A group has one chat id shared by everybody in it, so a linked group
	// would let every member read the owner's goals and log check-ins as them.
	// Staying in the chat while ignoring it would leave that chat id available
	// to be linked later, which is the same hole with extra steps.
	leaveChat
)

func (i intent) String() string {
	switch i {
	case answerUpdate:
		return "answer"
	case leaveChat:
		return "leave"
	default:
		return "ignore"
	}
}

// intentFor decides what to do about the chat an update arrived from.
//
// Deliberately fails closed: an unrecognised type is neither answered nor left.
// Answering would defeat the guard the moment Telegram adds a chat type, and
// leaving a chat North cannot identify is its own kind of wrong.
func intentFor(c chat) intent {
	switch c.Type {
	case chatPrivate:
		return answerUpdate
	case chatGroup, chatSupergroup, chatChannel:
		return leaveChat
	default:
		return ignoreUpdate
	}
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
// (empty for an ordinary message), and what to do with it.
//
// An update carrying no text and no photo — a sticker, a member joining — is
// not an error, it is simply not a question, and answering it would be worse
// than ignoring it.
//
// The message is populated even when the intent is leaveChat, because the
// caller still needs the chat id to leave it.
func (u update) inbound() (messaging.InboundMessage, string, intent) {
	switch {
	case u.CallbackQuery != nil && u.CallbackQuery.Message != nil:
		from := u.CallbackQuery.Message.Chat
		return messaging.InboundMessage{
			Platform:   messaging.PlatformTelegram,
			ExternalID: chatID(from.ID),
			Text:       u.CallbackQuery.Data,
			UpdateID:   u.UpdateID,
			ReceivedAt: time.Now(),
		}, u.CallbackQuery.ID, intentFor(from)

	case u.Message != nil:
		from := u.Message.Chat

		// The chat is checked before the text is: a group must be left whether
		// or not the message that revealed it happened to carry words.
		what := intentFor(from)
		attachment := photoAttachment(u.Message.Photo, u.Message.Document)
		text := u.Message.Text
		if text == "" {
			text = u.Message.Caption
		}
		if what == answerUpdate && text == "" && attachment == nil {
			what = ignoreUpdate
		}

		received := time.Now()
		if u.Message.Date > 0 {
			received = time.Unix(u.Message.Date, 0)
		}
		return messaging.InboundMessage{
			Platform:   messaging.PlatformTelegram,
			ExternalID: chatID(from.ID),
			Text:       text,
			Attachment: attachment,
			UpdateID:   u.UpdateID,
			ReceivedAt: received,
		}, "", what

	default:
		return messaging.InboundMessage{}, "", ignoreUpdate
	}
}

// chatID renders Telegram's numeric chat id as the text external id the
// messaging package stores. Text because the next platform's is a string.
func chatID(id int64) string {
	return strconv.FormatInt(id, 10)
}

func photoAttachment(photos []tgPhoto, doc *tgDoc) *messaging.InboundFile {
	if n := len(photos); n > 0 {
		// Last size is the largest. The others are Telegram's thumbnails.
		return &messaging.InboundFile{
			Kind:   "image",
			FileID: photos[n-1].FileID,
			Name:   "photo.jpg",
		}
	}
	if doc != nil && isImageMIME(doc.MIMEType) {
		name := doc.FileName
		if name == "" {
			name = "photo.jpg"
		}
		return &messaging.InboundFile{
			Kind:     "image",
			FileID:   doc.FileID,
			MIMEType: doc.MIMEType,
			Name:     name,
		}
	}
	return nil
}

func isImageMIME(mime string) bool {
	switch mime {
	case "image/jpeg", "image/png", "image/gif", "image/webp":
		return true
	default:
		return false
	}
}
