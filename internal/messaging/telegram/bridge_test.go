package telegram

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/messaging"
)

// slowHandler answers only once released, so a test can hold a turn open for
// as long as it needs the indicator to survive.
type slowHandler struct {
	release chan struct{}
}

func (s slowHandler) Handle(_ context.Context, _ messaging.InboundMessage) (messaging.OutboundMessage, error) {
	<-s.release
	return messaging.OutboundMessage{Text: "done"}, nil
}

// Telegram drops the typing indicator after about five seconds, and a coach
// turn takes far longer. One call would leave the person watching nothing
// happen, which is exactly when people decide a bot is broken.
func TestTypingIsRefreshedForTheWholeTurn(t *testing.T) {
	api := newBotAPI(t)
	handler := slowHandler{release: make(chan struct{})}

	b := &bridge{
		messages:    handler,
		client:      api.client(),
		log:         slog.Default(),
		typingEvery: 20 * time.Millisecond,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		b.answer(context.Background(), messaging.InboundMessage{
			Platform:   messaging.PlatformTelegram,
			ExternalID: "884422",
			Text:       "how am I doing?",
		}, "")
	}()

	// Long enough for several ticks.
	waitFor(t, func() bool { return len(api.method("sendChatAction")) >= 3 })

	close(handler.release)
	<-done

	if got := len(api.sends()); got != 1 {
		t.Fatalf("expected one reply, got %d", got)
	}
}

// And it stops: a ticker left running would keep poking Telegram for a chat
// that already has its answer.
func TestTypingStopsOnceTheReplyIsReady(t *testing.T) {
	api := newBotAPI(t)
	handler := slowHandler{release: make(chan struct{})}
	close(handler.release) // answer immediately

	b := &bridge{
		messages:    handler,
		client:      api.client(),
		log:         slog.Default(),
		typingEvery: 20 * time.Millisecond,
	}

	b.answer(context.Background(), messaging.InboundMessage{
		Platform:   messaging.PlatformTelegram,
		ExternalID: "884422",
		Text:       "hi",
	}, "")

	settled := len(api.method("sendChatAction"))
	time.Sleep(120 * time.Millisecond) // several ticks' worth

	if got := len(api.method("sendChatAction")); got != settled {
		t.Fatalf("typing kept going after the reply: %d then %d", settled, got)
	}
}

// okHandler answers immediately, for tests about what happens before the coach.
type okHandler struct{}

func (okHandler) Handle(_ context.Context, _ messaging.InboundMessage) (messaging.OutboundMessage, error) {
	return messaging.OutboundMessage{Text: "answered"}, nil
}

// A voice note is no longer downloaded here at all: the messaging service
// refuses an over-long one before the bytes move, and fetches them itself when
// the recording has earned it. So the bridge must leave it alone.
func TestAVoiceNoteIsNotDownloadedByTheBridge(t *testing.T) {
	api := newBotAPI(t)

	b := &bridge{
		messages:    okHandler{},
		client:      api.client(),
		log:         slog.Default(),
		typingEvery: time.Hour,
	}

	b.answer(context.Background(), messaging.InboundMessage{
		Platform:   messaging.PlatformTelegram,
		ExternalID: "884422",
		Attachment: &messaging.InboundFile{Kind: messaging.KindVoice, FileID: "AwACAgQAAx"},
	}, "")

	for _, call := range api.sent() {
		if call.method == "getFile" {
			t.Fatal("the bridge downloaded a voice note; that belongs to the service now")
		}
	}
	if len(api.sends()) != 1 {
		t.Fatalf("expected the reply to go out anyway, got %d sends", len(api.sends()))
	}
}

// And a photo still says photo. The fix must not make every failure generic.
func TestAFailedPhotoDownloadStillMentionsAPhoto(t *testing.T) {
	api := newBotAPI(t)

	b := &bridge{
		messages:    okHandler{},
		client:      api.client(),
		log:         slog.Default(),
		typingEvery: time.Hour,
	}

	b.answer(context.Background(), messaging.InboundMessage{
		Platform:   messaging.PlatformTelegram,
		ExternalID: "884422",
		Attachment: &messaging.InboundFile{Kind: messaging.KindImage, FileID: "AgACAgQAAx"},
	}, "")

	sends := api.sends()
	if len(sends) != 1 {
		t.Fatalf("expected one reply, got %d", len(sends))
	}
	text, _ := sends[0].body["text"].(string)
	if !strings.Contains(strings.ToLower(text), "photo") {
		t.Fatalf("a failed photo download did not mention a photo: %q", text)
	}
}
