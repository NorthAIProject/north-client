package telegram

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/messaging"
)

// A group chat must never reach the coach.
//
// This is the account-takeover path: a group has one chat id shared by every
// member, so a linked group would let all of them read the owner's goals and
// log check-ins as them. The bot leaves rather than merely ignoring, because a
// chat it stays in is a chat that could later be linked.
func TestGroupChatsAreLeftRatherThanAnswered(t *testing.T) {
	for _, chatType := range []string{"group", "supergroup", "channel"} {
		raw := []byte(`{"update_id":1,"message":{"chat":{"id":-1001234,"type":"` + chatType + `"},"text":"hello","date":1755300000}}`)

		u, ok := decodeUpdate(raw)
		if !ok {
			t.Fatalf("%s: update did not parse", chatType)
		}

		msg, _, got := u.inbound()
		if got != leaveChat {
			t.Fatalf("%s: intent = %v, want leaveChat", chatType, got)
		}
		if msg.ExternalID != "-1001234" {
			t.Fatalf("%s: the chat to leave was lost: %q", chatType, msg.ExternalID)
		}
	}
}

func TestPrivateChatsAreAnswered(t *testing.T) {
	raw := []byte(`{"update_id":2,"message":{"chat":{"id":884422,"type":"private"},"text":"how am I doing?","date":1755300000}}`)

	u, ok := decodeUpdate(raw)
	if !ok {
		t.Fatal("update did not parse")
	}

	msg, callbackID, got := u.inbound()
	if got != answerUpdate {
		t.Fatalf("intent = %v, want answerUpdate", got)
	}
	if callbackID != "" {
		t.Fatalf("an ordinary message has no callback id, got %q", callbackID)
	}
	if msg.ExternalID != "884422" || msg.Text != "how am I doing?" || msg.UpdateID != 2 {
		t.Fatalf("decoded wrong: %+v", msg)
	}
	if msg.Platform != messaging.PlatformTelegram {
		t.Fatalf("platform = %q", msg.Platform)
	}
}

// Fail closed. A chat whose type Khepri does not recognise is not assumed to be
// private, and is not left either — leaving a chat that cannot be identified
// would be its own kind of wrong.
func TestAnUnknownChatTypeIsIgnored(t *testing.T) {
	for _, body := range []string{
		`{"update_id":3,"message":{"chat":{"id":1,"type":"something_new"},"text":"hi"}}`,
		`{"update_id":4,"message":{"chat":{"id":1},"text":"hi"}}`,
	} {
		u, ok := decodeUpdate([]byte(body))
		if !ok {
			t.Fatalf("%s: did not parse", body)
		}
		if _, _, got := u.inbound(); got != ignoreUpdate {
			t.Fatalf("%s: intent = %v, want ignoreUpdate", body, got)
		}
	}
}

func TestACallbackFromAGroupIsAlsoRefused(t *testing.T) {
	raw := []byte(`{"update_id":5,"callback_query":{"id":"cb-1","data":"approve","message":{"chat":{"id":-100999,"type":"supergroup"}}}}`)

	u, ok := decodeUpdate(raw)
	if !ok {
		t.Fatal("update did not parse")
	}
	if _, _, got := u.inbound(); got != leaveChat {
		t.Fatalf("intent = %v, want leaveChat", got)
	}
}

func TestACallbackFromAPrivateChatIsAnswered(t *testing.T) {
	raw := []byte(`{"update_id":6,"callback_query":{"id":"cb-1","data":"approve","message":{"chat":{"id":884422,"type":"private"}}}}`)

	u, ok := decodeUpdate(raw)
	if !ok {
		t.Fatal("update did not parse")
	}

	msg, callbackID, got := u.inbound()
	if got != answerUpdate {
		t.Fatalf("intent = %v, want answerUpdate", got)
	}
	if callbackID != "cb-1" {
		t.Fatalf("callback id = %q", callbackID)
	}
	if msg.Text != messaging.AnswerApprove {
		t.Fatalf("button value = %q", msg.Text)
	}
}

func TestAPrivatePhotoIsAnswered(t *testing.T) {
	raw := []byte(`{"update_id":9,"message":{"chat":{"id":884422,"type":"private"},"caption":"how's my squat?","photo":[{"file_id":"small","width":90,"height":90},{"file_id":"large","width":1280,"height":1280}],"date":1755300000}}`)

	u, ok := decodeUpdate(raw)
	if !ok {
		t.Fatal("update did not parse")
	}
	msg, _, got := u.inbound()
	if got != answerUpdate {
		t.Fatalf("intent = %v, want answerUpdate", got)
	}
	if msg.Text != "how's my squat?" {
		t.Fatalf("caption = %q", msg.Text)
	}
	if msg.Attachment == nil || msg.Attachment.FileID != "large" {
		t.Fatalf("attachment = %+v, want the largest photo", msg.Attachment)
	}
}

func TestAPhotoWithNoCaptionIsStillAQuestion(t *testing.T) {
	raw := []byte(`{"update_id":10,"message":{"chat":{"id":884422,"type":"private"},"photo":[{"file_id":"p1","width":100,"height":100}]}}`)

	u, ok := decodeUpdate(raw)
	if !ok {
		t.Fatal("update did not parse")
	}
	msg, _, got := u.inbound()
	if got != answerUpdate {
		t.Fatalf("intent = %v, want answerUpdate", got)
	}
	if msg.Attachment == nil {
		t.Fatal("photo was dropped")
	}
}

func TestUpdatesWithNothingToAnswerAreIgnored(t *testing.T) {
	for _, body := range []string{
		`{"update_id":7,"message":{"chat":{"id":884422,"type":"private"},"date":1755300000}}`,
		`{"update_id":8}`,
	} {
		u, ok := decodeUpdate([]byte(body))
		if !ok {
			t.Fatalf("%s: did not parse", body)
		}
		if _, _, got := u.inbound(); got != ignoreUpdate {
			t.Fatalf("%s: intent = %v, want ignoreUpdate", body, got)
		}
	}
}

func TestMalformedJSONDoesNotParse(t *testing.T) {
	if _, ok := decodeUpdate([]byte("not json at all")); ok {
		t.Fatal("malformed JSON parsed")
	}
}

// A voice note is the whole point of decoding this field: somebody on a phone
// says a sentence instead of typing it, and the answer must be the one they
// would have got for typing it.
func TestAVoiceNoteBecomesAnInboundFile(t *testing.T) {
	raw := []byte(`{"update_id":9,"message":{"chat":{"id":884422,"type":"private"},"date":1755300000,` +
		`"voice":{"file_id":"AwACAgQAAx","duration":7,"mime_type":"audio/ogg","file_size":8123}}}`)

	u, ok := decodeUpdate(raw)
	if !ok {
		t.Fatal("update did not parse")
	}

	msg, _, got := u.inbound()
	if got != answerUpdate {
		t.Fatalf("intent = %v, want answerUpdate", got)
	}
	if msg.Attachment == nil {
		t.Fatal("no attachment; a voice note was dropped")
	}
	if msg.Attachment.Kind != messaging.KindVoice {
		t.Fatalf("kind = %q, want %q", msg.Attachment.Kind, messaging.KindVoice)
	}
	if msg.Attachment.FileID != "AwACAgQAAx" {
		t.Fatalf("file id = %q", msg.Attachment.FileID)
	}
	if msg.Attachment.MIMEType != "audio/ogg" {
		t.Fatalf("mime = %q, want the declared audio/ogg as a hint", msg.Attachment.MIMEType)
	}
	if msg.Attachment.DurationSeconds != 7 {
		t.Fatalf("duration = %d, want 7", msg.Attachment.DurationSeconds)
	}
}

// A voice note carries no text, and that must not make it look like an empty
// message. Before this field was decoded, exactly this update was ignored.
func TestAVoiceNoteWithNoTextIsNotIgnored(t *testing.T) {
	raw := []byte(`{"update_id":10,"message":{"chat":{"id":884422,"type":"private"},"date":1755300000,` +
		`"voice":{"file_id":"AwACAgQAAx","duration":3,"mime_type":"audio/ogg"}}}`)

	u, _ := decodeUpdate(raw)
	if _, _, got := u.inbound(); got != answerUpdate {
		t.Fatalf("intent = %v, want answerUpdate", got)
	}
}

// Fail closed, the way intentFor does. A forwarded song is not dictation, and
// an hour of it would be an hour of transcription bought by one tap.
func TestAForwardedAudioFileIsIgnored(t *testing.T) {
	raw := []byte(`{"update_id":11,"message":{"chat":{"id":884422,"type":"private"},"date":1755300000,` +
		`"audio":{"file_id":"CQACAgQAAx","duration":420,"mime_type":"audio/mpeg","title":"a whole album"}}}`)

	u, _ := decodeUpdate(raw)
	msg, _, got := u.inbound()
	if got != ignoreUpdate {
		t.Fatalf("intent = %v, want ignoreUpdate", got)
	}
	if msg.Attachment != nil {
		t.Fatalf("attachment = %+v, want none", msg.Attachment)
	}
}

// A video note is a round video, not a dictation. Same reasoning.
func TestAVideoNoteIsIgnored(t *testing.T) {
	raw := []byte(`{"update_id":12,"message":{"chat":{"id":884422,"type":"private"},"date":1755300000,` +
		`"video_note":{"file_id":"DQACAgQAAx","duration":9}}}`)

	u, _ := decodeUpdate(raw)
	if _, _, got := u.inbound(); got != ignoreUpdate {
		t.Fatalf("intent = %v, want ignoreUpdate", got)
	}
}

// A voice note sent to a group is still a group message, and the bot leaves.
// The chat is checked before the content, and this proves the new field did not
// slip in front of that.
func TestAVoiceNoteInAGroupIsStillLeft(t *testing.T) {
	raw := []byte(`{"update_id":13,"message":{"chat":{"id":-100999,"type":"group"},"date":1755300000,` +
		`"voice":{"file_id":"AwACAgQAAx","duration":4,"mime_type":"audio/ogg"}}}`)

	u, _ := decodeUpdate(raw)
	if _, _, got := u.inbound(); got != leaveChat {
		t.Fatalf("intent = %v, want leaveChat", got)
	}
}
