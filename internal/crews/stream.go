package crews

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/crews/live"
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

// viewerGate applies what is true for every stream: a message from someone
// blocked either way never reaches the viewer, and the viewer's own removal
// ends the stream. name turns a passed event into the frame to send.
func (c *Chat) viewerGate(viewerID uuid.UUID, name func(live.Event) (frame, bool)) gate {
	return func(ctx context.Context, ev live.Event) (frame, bool, bool) {
		switch ev.Type {
		case live.EventMessage, live.EventUpdate:
			blocked, err := c.repo.AuthorBlockedFor(ctx, viewerID, ev.MessageID)
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

// apiFrame is an event as the app reads it: its type as the event name and
// the event itself as JSON.
func apiFrame(ev live.Event) (frame, bool) {
	data, err := json.Marshal(ev)
	if err != nil {
		return frame{}, false
	}
	return frame{event: ev.Type, data: string(data)}, true
}
