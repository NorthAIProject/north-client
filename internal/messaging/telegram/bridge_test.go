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

// A download that fails must apologise for the thing the person actually sent.
// Telling somebody who recorded their voice that a photo failed is a second
// bug on top of the first, and the one they will report.
func TestAFailedVoiceDownloadDoesNotMentionAPhoto(t *testing.T) {
	// No getFile route: every download fails.
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

	sends := api.sends()
	if len(sends) != 1 {
		t.Fatalf("expected one reply, got %d", len(sends))
	}
	text, _ := sends[0].body["text"].(string)
	if text == "" {
		t.Fatal("a failed download said nothing")
	}
	if strings.Contains(strings.ToLower(text), "photo") {
		t.Fatalf("a failed voice note apologised for a photo: %q", text)
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
