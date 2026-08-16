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

// Fail closed. A chat whose type North does not recognise is not assumed to be
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
