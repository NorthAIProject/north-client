package live_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/crews/live"
)

func next(t *testing.T, ch <-chan live.Event) (live.Event, bool) {
	t.Helper()
	select {
	case ev, ok := <-ch:
		return ev, ok
	case <-time.After(time.Second):
		t.Fatal("no event within a second")
		return live.Event{}, false
	}
}

func empty(t *testing.T, ch <-chan live.Event) {
	t.Helper()
	select {
	case ev := <-ch:
		t.Fatalf("unexpected event %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestHubFanOutPerCrew(t *testing.T) {
	h := live.NewHub()
	a, b := uuid.New(), uuid.New()
	a1, cancelA1 := h.Subscribe(a)
	defer cancelA1()
	a2, cancelA2 := h.Subscribe(a)
	defer cancelA2()
	b1, cancelB1 := h.Subscribe(b)
	defer cancelB1()

	h.Publish(live.Event{Type: live.EventMessage, CrewID: a, Seq: 7})
	for _, ch := range []<-chan live.Event{a1, a2} {
		if ev, _ := next(t, ch); ev.Seq != 7 {
			t.Fatalf("got %+v", ev)
		}
	}
	empty(t, b1)

	cancelA2()
	h.Publish(live.Event{Type: live.EventMessage, CrewID: a, Seq: 8})
	if ev, _ := next(t, a1); ev.Seq != 8 {
		t.Fatalf("got %+v", ev)
	}
}

// A subscriber that stops reading never blocks a publisher; what it missed
// becomes one resync.
func TestHubSlowSubscriberGetsResync(t *testing.T) {
	h := live.NewHub()
	crew := uuid.New()
	ch, cancel := h.Subscribe(crew)
	defer cancel()

	done := make(chan struct{})
	go func() {
		for i := range 40 {
			h.Publish(live.Event{Type: live.EventMessage, CrewID: crew, Seq: int64(i + 1)})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a full subscriber")
	}

	var last live.Event
	for drained := false; !drained; {
		select {
		case last = <-ch:
		default:
			drained = true
		}
	}
	if last.Type != live.EventResync {
		t.Fatalf("last event = %+v; want resync", last)
	}
}

func TestHubResyncAndClose(t *testing.T) {
	h := live.NewHub()
	ch, cancel := h.Subscribe(uuid.New())
	defer cancel()
	h.Resync()
	if ev, _ := next(t, ch); ev.Type != live.EventResync {
		t.Fatalf("got %+v", ev)
	}
	h.Close()
	if _, ok := next(t, ch); ok {
		t.Fatal("channel still open after Close")
	}
	late, lateCancel := h.Subscribe(uuid.New())
	defer lateCancel()
	if _, ok := next(t, late); ok {
		t.Fatal("subscribing after Close gave an open channel")
	}
}
