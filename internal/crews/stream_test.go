package crews_test

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/crews"
	"github.com/NorthAIProject/north-client/internal/crews/live"
	"github.com/NorthAIProject/north-client/internal/social"
	"github.com/NorthAIProject/north-client/internal/users"
)

// asUser puts the user named by the X-Test-User header in the context, as
// the auth middleware would.
func asUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := uuid.Parse(r.Header.Get("X-Test-User"))
		next.ServeHTTP(w, r.WithContext(auth.ContextWithUser(r.Context(), users.User{ID: id})))
	})
}

type liveServer struct {
	hub *live.Hub
	api *httptest.Server
	web *httptest.Server
}

func newLiveServer(t *testing.T, r room) liveServer {
	t.Helper()
	hub := live.NewHub()
	t.Cleanup(hub.Close)
	api := chi.NewRouter()
	api.Use(asUser)
	crews.NewAPI(r.svc, "https://north.test").WithLive(r.chat, hub).WithHeartbeat(50 * time.Millisecond).Routes(api)
	web := chi.NewRouter()
	web.Use(asUser)
	crews.NewHandler(r.svc, "https://north.test", false).WithLive(r.chat, hub).WithHeartbeat(50 * time.Millisecond).Routes(web)
	s := liveServer{hub: hub, api: httptest.NewServer(api), web: httptest.NewServer(web)}
	t.Cleanup(s.api.Close)
	t.Cleanup(s.web.Close)
	return s
}

// stream is an open event stream and its frames, one "event|data" string
// per frame; a ping comment comes through as "ping".
type stream struct {
	resp   *http.Response
	frames chan string
}

func open(t *testing.T, base, path string, as users.User) (*http.Response, *stream) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	req.Header.Set("X-Test-User", as.ID.String())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	s := &stream{resp: resp, frames: make(chan string, 64)}
	go func() {
		defer close(s.frames)
		br := bufio.NewReader(resp.Body)
		var event, data string
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\n")
			switch {
			case line == ": ping":
				s.frames <- "ping"
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data = strings.TrimPrefix(line, "data: ")
			case line == "" && event != "":
				s.frames <- event + "|" + data
				event, data = "", ""
			}
		}
	}()
	return resp, s
}

// expect waits for the first frame that is not a ping and has event name.
func (s *stream) expect(t *testing.T, name string) string {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case f, ok := <-s.frames:
			if !ok {
				t.Fatalf("stream ended waiting for %q", name)
			}
			if f == "ping" {
				continue
			}
			event, data, _ := strings.Cut(f, "|")
			if event != name {
				t.Fatalf("got %q; want event %q", f, name)
			}
			return data
		case <-deadline:
			t.Fatalf("no %q within 2s", name)
		}
	}
}

// quiet asserts no event frame arrives for a moment; pings are fine.
func (s *stream) quiet(t *testing.T) {
	t.Helper()
	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case f := <-s.frames:
			if f != "ping" {
				t.Fatalf("unexpected frame %q", f)
			}
		case <-deadline:
			return
		}
	}
}

// subscribed waits until the hub has the stream's subscription, so a
// publish right after open is not lost: the first ping proves the handler
// is in its loop.
func (s *stream) subscribed(t *testing.T) {
	t.Helper()
	select {
	case f := <-s.frames:
		if f != "ping" {
			t.Fatalf("first frame %q; want a ping", f)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no ping")
	}
}

func TestLiveRejectsNonMember(t *testing.T) {
	r := newRoom(t)
	s := newLiveServer(t, r)
	zed := handled(t, r.pool, "zed@north.test", "Zed", "zed")
	resp, _ := open(t, s.api.URL, "/crews/"+r.crew.ID.String()+"/live", zed)
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status %d; want 404", resp.StatusCode)
	}
}

func TestLiveAPIStreamsEvents(t *testing.T) {
	r := newRoom(t)
	s := newLiveServer(t, r)
	resp, st := open(t, s.api.URL, "/crews/"+r.crew.ID.String()+"/live", r.ana)
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content type %q", ct)
	}
	st.subscribed(t)
	m := r.send(t, r.leo, "hi")
	s.hub.Publish(live.Event{Type: live.EventMessage, CrewID: r.crew.ID, ChannelID: r.general, MessageID: m.ID, Seq: m.Seq})
	data := st.expect(t, "message")
	if !strings.Contains(data, `"messageId":"`+m.ID.String()+`"`) {
		t.Fatalf("data %s", data)
	}
}

func TestLiveDropsBlockedAuthor(t *testing.T) {
	r := newRoom(t)
	s := newLiveServer(t, r)
	if err := social.NewRepository(r.pool).Block(context.Background(), r.ana.ID, r.leo.ID); err != nil {
		t.Fatal(err)
	}
	_, anas := open(t, s.api.URL, "/crews/"+r.crew.ID.String()+"/live", r.ana)
	_, owners := open(t, s.api.URL, "/crews/"+r.crew.ID.String()+"/live", r.owner)
	anas.subscribed(t)
	owners.subscribed(t)
	m := r.send(t, r.leo, "from leo")
	s.hub.Publish(live.Event{Type: live.EventMessage, CrewID: r.crew.ID, ChannelID: r.general, MessageID: m.ID, Seq: m.Seq})
	owners.expect(t, "message")
	anas.quiet(t)
}

func TestLiveRemovedClosesStream(t *testing.T) {
	r := newRoom(t)
	s := newLiveServer(t, r)
	_, anas := open(t, s.api.URL, "/crews/"+r.crew.ID.String()+"/live", r.ana)
	_, leos := open(t, s.api.URL, "/crews/"+r.crew.ID.String()+"/live", r.leo)
	anas.subscribed(t)
	leos.subscribed(t)
	if err := r.svc.Remove(context.Background(), r.crew.ID, r.owner.ID, r.ana.ID); err != nil {
		t.Fatal(err)
	}
	s.hub.Publish(live.Event{Type: live.EventRemoved, CrewID: r.crew.ID, UserID: r.ana.ID})
	anas.expect(t, "removed")
	select {
	case f, ok := <-anas.frames:
		if ok {
			t.Fatalf("frame %q after removed; want the stream closed", f)
		}
	case <-time.After(time.Second):
		t.Fatal("stream still open a second after removed")
	}
	leos.expect(t, "channels")
}

func TestLiveHeartbeat(t *testing.T) {
	r := newRoom(t)
	s := newLiveServer(t, r)
	_, st := open(t, s.api.URL, "/crews/"+r.crew.ID.String()+"/live", r.ana)
	st.subscribed(t)
}

func TestLiveWebEventNames(t *testing.T) {
	r := newRoom(t)
	s := newLiveServer(t, r)
	other, err := r.svc.CreateChannel(context.Background(), r.crew.ID, r.owner.ID, "legs", "")
	if err != nil {
		t.Fatal(err)
	}
	_, st := open(t, s.web.URL, "/crews/"+r.crew.ID.String()+"/live?channel="+r.general.String(), r.ana)
	st.subscribed(t)

	here := r.send(t, r.leo, "here")
	s.hub.Publish(live.Event{Type: live.EventMessage, CrewID: r.crew.ID, ChannelID: r.general, MessageID: here.ID, Seq: here.Seq})
	st.expect(t, "crew-new")

	there, err := r.chat.Send(context.Background(), r.crew.ID, other.ID, r.leo.ID, crews.SendInput{Body: "there"})
	if err != nil {
		t.Fatal(err)
	}
	s.hub.Publish(live.Event{Type: live.EventMessage, CrewID: r.crew.ID, ChannelID: other.ID, MessageID: there.ID, Seq: there.Seq})
	st.expect(t, "crew-unread")

	s.hub.Publish(live.Event{Type: live.EventUpdate, CrewID: r.crew.ID, ChannelID: r.general, MessageID: here.ID})
	st.expect(t, "msg-"+here.ID.String())

	s.hub.Publish(live.Event{Type: live.EventResync, CrewID: r.crew.ID})
	st.expect(t, "crew-resync")
}
