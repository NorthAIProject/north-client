package live

import (
	"sync"

	"github.com/google/uuid"
)

// buffer is how many events one subscriber may fall behind before what it
// missed collapses into a resync.
const buffer = 32

// Hub fans events out to the streams watching each crew, in this process.
type Hub struct {
	mu     sync.Mutex
	subs   map[uuid.UUID]map[*subscriber]struct{}
	closed bool
}

type subscriber struct {
	ch     chan Event
	closed bool
}

func NewHub() *Hub { return &Hub{subs: map[uuid.UUID]map[*subscriber]struct{}{}} }

// Subscribe watches crewID. The channel closes when cancel is called or the
// hub closes; after Close it is closed from the start.
func (h *Hub) Subscribe(crewID uuid.UUID) (events <-chan Event, cancel func()) {
	s := &subscriber{ch: make(chan Event, buffer)}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		close(s.ch)
		return s.ch, func() {}
	}
	if h.subs[crewID] == nil {
		h.subs[crewID] = map[*subscriber]struct{}{}
	}
	h.subs[crewID][s] = struct{}{}
	return s.ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.subs[crewID], s)
		if len(h.subs[crewID]) == 0 {
			delete(h.subs, crewID)
		}
		s.close()
	}
}

// Publish hands ev to everyone watching its crew. It never blocks.
func (h *Hub) Publish(ev Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.subs[ev.CrewID] {
		s.offer(ev)
	}
}

// Resync tells every subscriber to refetch: events may have been missed,
// as when the listener reconnects.
func (h *Hub) Resync() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for crewID, subs := range h.subs {
		for s := range subs {
			s.offer(Event{Type: EventResync, CrewID: crewID})
		}
	}
}

// Close ends every stream, for shutdown.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for _, subs := range h.subs {
		for s := range subs {
			s.close()
		}
	}
	h.subs = map[uuid.UUID]map[*subscriber]struct{}{}
}

// offer queues ev without blocking. A full buffer means the reader is behind:
// the oldest event makes room for a resync, which tells it to refetch
// everything it may have missed.
func (s *subscriber) offer(ev Event) {
	if s.closed {
		return
	}
	select {
	case s.ch <- ev:
		return
	default:
	}
	select {
	case <-s.ch:
	default:
	}
	select {
	case s.ch <- Event{Type: EventResync, CrewID: ev.CrewID}:
	default:
	}
}

func (s *subscriber) close() {
	if !s.closed {
		s.closed = true
		close(s.ch)
	}
}
