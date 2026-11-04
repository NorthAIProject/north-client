package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/messaging"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/lifecycle"
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
		})
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
	})

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
	})

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
	})

	sends := api.sends()
	if len(sends) != 1 {
		t.Fatalf("expected one reply, got %d", len(sends))
	}
	text, _ := sends[0].body["text"].(string)
	if !strings.Contains(strings.ToLower(text), "photo") {
		t.Fatalf("a failed photo download did not mention a photo: %q", text)
	}
}

type errHandler struct{ err error }

func (h errHandler) Handle(_ context.Context, _ messaging.InboundMessage) (messaging.OutboundMessage, error) {
	return messaging.OutboundMessage{}, h.err
}

func TestAPaymentRefusalSaysTheServiceIsOutOfCredit(t *testing.T) {
	api := newBotAPI(t)

	b := &bridge{
		messages:    errHandler{err: apperr.Wrap(apperr.ErrPaymentRequired, "openrouter")},
		client:      api.client(),
		log:         slog.Default(),
		typingEvery: time.Hour,
	}

	b.answer(context.Background(), messaging.InboundMessage{
		Platform:   messaging.PlatformTelegram,
		ExternalID: "884422",
		Text:       "how am I doing?",
	})

	sends := api.sends()
	if len(sends) != 1 {
		t.Fatalf("expected one reply, got %d", len(sends))
	}
	text, _ := sends[0].body["text"].(string)
	if !strings.Contains(strings.ToLower(text), "credit") {
		t.Fatalf("a payment refusal did not mention credit: %q", text)
	}
	if strings.Contains(text, "Something went wrong on my side") {
		t.Fatalf("a payment refusal used the generic apology: %q", text)
	}
}

func TestAnyOtherFailureStillApologises(t *testing.T) {
	api := newBotAPI(t)

	b := &bridge{
		messages:    errHandler{err: errors.New("could not load the linked user")},
		client:      api.client(),
		log:         slog.Default(),
		typingEvery: time.Hour,
	}

	b.answer(context.Background(), messaging.InboundMessage{
		Platform:   messaging.PlatformTelegram,
		ExternalID: "884422",
		Text:       "hi",
	})

	sends := api.sends()
	if len(sends) != 1 {
		t.Fatalf("expected one reply, got %d", len(sends))
	}
	text, _ := sends[0].body["text"].(string)
	if text != "Something went wrong on my side. Try again in a moment." {
		t.Fatalf("generic failure = %q", text)
	}
}

// A file the service will refuse anyway is not pulled across the network
// first: the refusal needs only the name and size Telegram already sent.
func TestARefusedDocumentIsNotDownloadedByTheBridge(t *testing.T) {
	for _, file := range []messaging.InboundFile{
		{Kind: messaging.KindFile, FileID: "big", Name: "dieta.pdf", SizeBytes: 9 << 20},
		{Kind: messaging.KindFile, FileID: "zip", Name: "photos.zip", SizeBytes: 1 << 10},
	} {
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
			Attachment: &file,
		})

		for _, call := range api.sent() {
			if call.method == "getFile" {
				t.Fatalf("the bridge downloaded %s, which the service refuses", file.Name)
			}
		}
	}
}

// handlerFunc lets a test say what the messaging service does in one place.
type handlerFunc func(context.Context, messaging.InboundMessage) (messaging.OutboundMessage, error)

func (f handlerFunc) Handle(ctx context.Context, in messaging.InboundMessage) (messaging.OutboundMessage, error) {
	return f(ctx, in)
}

// textUpdate is a private-chat message as Telegram delivers it.
func textUpdate(updateID int64, chat int64, text string) []byte {
	return fmt.Appendf(nil, `{"update_id":%d,"message":{"chat":{"id":%d,"type":"private"},"text":%q}}`, updateID, chat, text)
}

// Two turns at once in one chat would each answer without seeing the other, and
// a photo followed by a question is one thought in two updates. So a chat gets
// one turn at a time, in the order it arrived, and no chat is remembered after
// its last turn.
func TestTurnsInOneChatRunOneAtATimeInArrivalOrder(t *testing.T) {
	api := newBotAPI(t)
	release := make(chan struct{})

	var (
		mu       sync.Mutex
		order    []string
		inFlight int
		highest  int
	)
	handler := handlerFunc(func(_ context.Context, in messaging.InboundMessage) (messaging.OutboundMessage, error) {
		mu.Lock()
		order = append(order, in.Text)
		inFlight++
		highest = max(highest, inFlight)
		mu.Unlock()

		<-release

		mu.Lock()
		inFlight--
		mu.Unlock()
		return messaging.OutboundMessage{Text: "answered"}, nil
	})

	b := &bridge{messages: handler, client: api.client(), log: slog.Default(), typingEvery: time.Hour}
	want := []string{"one", "two", "three", "four", "five"}
	for i, text := range want {
		b.dispatchRaw(context.Background(), textUpdate(int64(100+i), 884422, text))
	}

	// Time for any turn that wrongly runs alongside the first to start.
	time.Sleep(100 * time.Millisecond)
	close(release)

	waitFor(t, func() bool { return len(api.sends()) == len(want) })
	waitFor(t, func() bool { return b.chats.busyChats() == 0 })

	mu.Lock()
	defer mu.Unlock()
	if highest != 1 {
		t.Errorf("%d turns ran at once in one chat, want 1", highest)
	}
	if !slices.Equal(order, want) {
		t.Errorf("turns ran as %v, want %v", order, want)
	}
}

// One person waiting on a long turn must not hold up anybody else.
func TestABusyChatDoesNotHoldUpAnother(t *testing.T) {
	api := newBotAPI(t)
	release := make(chan struct{})
	defer close(release)

	otherAnswered := make(chan struct{})
	handler := handlerFunc(func(_ context.Context, in messaging.InboundMessage) (messaging.OutboundMessage, error) {
		if in.ExternalID == "111" {
			<-release
		} else {
			close(otherAnswered)
		}
		return messaging.OutboundMessage{Text: "answered"}, nil
	})

	b := &bridge{messages: handler, client: api.client(), log: slog.Default(), typingEvery: time.Hour}
	b.dispatchRaw(context.Background(), textUpdate(1, 111, "slow"))
	b.dispatchRaw(context.Background(), textUpdate(2, 222, "quick"))

	select {
	case <-otherAnswered:
	case <-time.After(2 * time.Second):
		t.Fatal("a second chat waited on the first")
	}
}

// A goroutine started here is outside Echo's recover, so an unrecovered panic
// would end the process and every turn in it, and would strand the rest of this
// chat's queue.
func TestAPanickingTurnDoesNotStopTheNextOne(t *testing.T) {
	api := newBotAPI(t)

	answered := make(chan string, 1)
	handler := handlerFunc(func(_ context.Context, in messaging.InboundMessage) (messaging.OutboundMessage, error) {
		if in.Text == "boom" {
			panic("a turn went wrong")
		}
		answered <- in.Text
		return messaging.OutboundMessage{Text: "answered"}, nil
	})

	b := &bridge{messages: handler, client: api.client(), log: slog.New(slog.DiscardHandler), typingEvery: time.Hour}
	b.dispatchRaw(context.Background(), textUpdate(1, 884422, "boom"))
	b.dispatchRaw(context.Background(), textUpdate(2, 884422, "after"))

	select {
	case got := <-answered:
		if got != "after" {
			t.Fatalf("answered %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the turn after a panic never ran")
	}
	waitFor(t, func() bool { return b.chats.busyChats() == 0 })
}

// The spinner on a tapped button belongs to the tap, not to the turn. Queued
// behind a turn that takes minutes, it would look like the button was broken.
func TestATappedButtonStopsSpinningWhileAnEarlierTurnRuns(t *testing.T) {
	api := newBotAPI(t)
	release := make(chan struct{})
	defer close(release)

	handler := handlerFunc(func(context.Context, messaging.InboundMessage) (messaging.OutboundMessage, error) {
		<-release
		return messaging.OutboundMessage{Text: "answered"}, nil
	})

	b := &bridge{messages: handler, client: api.client(), log: slog.Default(), typingEvery: time.Hour}
	b.dispatchRaw(context.Background(), textUpdate(1, 884422, "a long question"))
	b.dispatchRaw(context.Background(), []byte(
		`{"update_id":2,"callback_query":{"id":"cb-1","data":"approve","message":{"chat":{"id":884422,"type":"private"}}}}`))

	waitFor(t, func() bool { return len(api.method("answerCallbackQuery")) == 1 })
}

// The budget covers answering, not waiting. If the clock started on arrival, a
// message behind a slow turn would run out of time before it was ever looked at.
func TestAQueuedTurnGetsItsFullTimeBudget(t *testing.T) {
	api := newBotAPI(t)
	release := make(chan struct{})
	deadlines := make(chan time.Time, 1)

	handler := handlerFunc(func(ctx context.Context, in messaging.InboundMessage) (messaging.OutboundMessage, error) {
		if in.Text == "first" {
			<-release
			return messaging.OutboundMessage{Text: "answered"}, nil
		}
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Error("a turn ran with no deadline")
		}
		deadlines <- deadline
		return messaging.OutboundMessage{Text: "answered"}, nil
	})

	b := &bridge{messages: handler, client: api.client(), log: slog.Default(), typingEvery: time.Hour}
	b.dispatchRaw(context.Background(), textUpdate(1, 884422, "first"))
	b.dispatchRaw(context.Background(), textUpdate(2, 884422, "second"))

	time.Sleep(50 * time.Millisecond)
	started := time.Now()
	close(release)

	select {
	case deadline := <-deadlines:
		if deadline.Before(started.Add(replyTimeout)) {
			t.Fatalf("the second turn lost %s of its budget to the queue", started.Add(replyTimeout).Sub(deadline))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the second turn never ran")
	}
	waitFor(t, func() bool { return b.chats.busyChats() == 0 })
}

// A shutdown must wait for the turn in progress and the ones queued behind it,
// or a rollout answers nobody.
func TestADrainWaitsForQueuedTurns(t *testing.T) {
	api := newBotAPI(t)
	tracker := lifecycle.NewTracker(slog.New(slog.DiscardHandler))

	handler := handlerFunc(func(context.Context, messaging.InboundMessage) (messaging.OutboundMessage, error) {
		time.Sleep(30 * time.Millisecond)
		return messaging.OutboundMessage{Text: "answered"}, nil
	})

	b := &bridge{messages: handler, client: api.client(), log: slog.Default(), typingEvery: time.Hour, background: tracker}
	b.dispatchRaw(context.Background(), textUpdate(1, 884422, "one"))
	b.dispatchRaw(context.Background(), textUpdate(2, 884422, "two"))

	if !tracker.Drain(5 * time.Second) {
		t.Fatal("the drain gave up")
	}
	if got := len(api.sends()); got != 2 {
		t.Fatalf("%d replies sent before the drain returned, want 2", got)
	}
}
