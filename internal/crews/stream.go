package crews

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/crews/live"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// heartbeat keeps a quiet stream open through proxies that drop idle
// connections (Traefik, Cloudflare).
const heartbeat = 20 * time.Second

// frame is one server-sent event. data may be empty: the web page only needs
// the event's name.
type frame struct {
	event string
	data  string
}

// gate decides what one viewer's stream does with an event: send this frame
// (or nothing), and whether the stream ends after it.
type gate func(ctx context.Context, ev live.Event) (f frame, send, end bool)

// serveLive streams events for crewID through g until the client leaves,
// the gate ends it, or the hub closes. The caller has checked membership.
func serveLive(w http.ResponseWriter, r *http.Request, hub *live.Hub, crewID uuid.UUID, g gate, every time.Duration) {
	events, cancel := hub.Subscribe(crewID)
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	// A ResponseController, because the logging middleware wraps the writer
	// and a type assertion to http.Flusher would fail.
	rc := http.NewResponseController(w)
	ping := func() bool {
		_, err := fmt.Fprint(w, ": ping\n\n")
		return err == nil && rc.Flush() == nil
	}
	// The first ping tells the client the subscription is in place.
	if !ping() {
		return
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if !ping() {
				return
			}
		case ev, open := <-events:
			if !open {
				return
			}
			f, send, end := g(ctx, ev)
			if send {
				if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", f.event, f.data); err != nil {
					return
				}
				if rc.Flush() != nil {
					return
				}
			}
			if end {
				return
			}
		}
	}
}

// BlockListRefresh is how long a stream trusts its copy of who is blocked.
//
// A block made while a stream is open therefore takes up to this long to reach
// it. That is safe to allow because an event is only a signal: the client fetches
// the message itself, and that read applies blocks at the moment it happens.
const BlockListRefresh = time.Minute

// blockList is one stream's copy of who its viewer has blocked or been blocked
// by. It belongs to the stream's goroutine, so it needs no lock.
//
// It exists because a query per subscriber per event is what makes a busy room
// fall behind, and a stream that falls behind starts dropping events.
type blockList struct {
	load     func(ctx context.Context) ([]uuid.UUID, error)
	now      func() time.Time
	ids      map[uuid.UUID]struct{}
	loadedAt time.Time
}

// has says whether id is blocked either way, refreshing a copy that has gone
// stale. On an error the caller drops the event, which is what happened when
// every event asked the database.
func (b *blockList) has(ctx context.Context, id uuid.UUID) (bool, error) {
	if b.ids == nil || b.now().Sub(b.loadedAt) > BlockListRefresh {
		ids, err := b.load(ctx)
		if err != nil {
			return false, err
		}
		b.ids = make(map[uuid.UUID]struct{}, len(ids))
		for _, blocked := range ids {
			b.ids[blocked] = struct{}{}
		}
		b.loadedAt = b.now()
	}
	_, blocked := b.ids[id]
	return blocked, nil
}

// forget makes the next check read the database again.
func (b *blockList) forget() { b.ids = nil }

// viewerGate applies what is true for every stream: a message from someone
// blocked either way never reaches the viewer, and the viewer's own removal
// ends the stream. name turns a passed event into the frame to send.
//
// A resync means events may have been lost, and a removal can be one of them, so
// it re-reads membership and the block list. A resync is also what a listener
// reconnect sends, which is when Postgres is most likely to be failing, so a
// check that fails does not end the stream: every stream in the process would
// reconnect at once. It stays owed, and the next event asks again.
func (c *Chat) viewerGate(viewerID uuid.UUID, name func(live.Event) (frame, bool)) gate {
	blocks := &blockList{
		load: func(ctx context.Context) ([]uuid.UUID, error) { return c.repo.BlockedEitherWay(ctx, viewerID) },
		now:  c.now,
	}
	owedMembershipCheck := false

	return func(ctx context.Context, ev live.Event) (frame, bool, bool) {
		if ev.Type == live.EventResync {
			blocks.forget()
			owedMembershipCheck = true
		}
		if owedMembershipCheck {
			_, _, err := c.member(ctx, ev.CrewID, viewerID)
			switch {
			case err == nil:
				owedMembershipCheck = false
			case apperr.Is(err, apperr.ErrNotFound):
				f, _ := name(live.Event{Type: live.EventRemoved, CrewID: ev.CrewID, UserID: viewerID})
				return f, true, true
			default:
				c.log.Warn("could not re-check crew membership on a live stream",
					slog.String("user_id", viewerID.String()), slog.Any("error", err))
			}
		}

		switch ev.Type {
		case live.EventMessage, live.EventUpdate:
			blocked, err := c.authorBlocked(ctx, blocks, viewerID, ev)
			if err != nil || blocked {
				return frame{}, false, false
			}
		case live.EventRemoved:
			if ev.UserID == viewerID {
				f, _ := name(ev)
				return f, true, true
			}
			// Someone else left: the member list changed.
			ev = live.Event{Type: live.EventChannels, CrewID: ev.CrewID}
		}
		f, send := name(ev)
		return f, send, false
	}
}

// authorBlocked says whether ev's author and the viewer have blocked each other.
//
// An event with no author falls back to asking about the message: the welcome
// message has none, and during a rolling deploy neither does an event written by
// a process that does not fill it in yet.
func (c *Chat) authorBlocked(ctx context.Context, blocks *blockList, viewerID uuid.UUID, ev live.Event) (bool, error) {
	if ev.AuthorID == uuid.Nil {
		return c.repo.AuthorBlockedFor(ctx, viewerID, ev.MessageID)
	}
	return blocks.has(ctx, ev.AuthorID)
}

// apiFrame is an event as the app reads it: its type as the event name and
// the event itself as JSON. The author stays behind: see live.Event.
func apiFrame(ev live.Event) (frame, bool) {
	ev.AuthorID = uuid.Nil
	data, err := json.Marshal(ev)
	if err != nil {
		return frame{}, false
	}
	return frame{event: ev.Type, data: string(data)}, true
}
