package crews_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/crews"
	"github.com/NorthAIProject/north-client/internal/crews/live"
	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/moderation"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	"github.com/NorthAIProject/north-client/internal/social"
	"github.com/NorthAIProject/north-client/internal/users"
)

// gateRepo counts what a stream's gate asks the database, and can make the
// membership check fail as it would during a Postgres blip.
type gateRepo struct {
	crews.ChatRepository
	authorChecks atomic.Int32
	blockLists   atomic.Int32
	memberFails  atomic.Bool
}

func (r *gateRepo) AuthorBlockedFor(ctx context.Context, viewerID, messageID uuid.UUID) (bool, error) {
	r.authorChecks.Add(1)
	return r.ChatRepository.AuthorBlockedFor(ctx, viewerID, messageID)
}

func (r *gateRepo) BlockedEitherWay(ctx context.Context, viewerID uuid.UUID) ([]uuid.UUID, error) {
	r.blockLists.Add(1)
	return r.ChatRepository.BlockedEitherWay(ctx, viewerID)
}

func (r *gateRepo) MemberRole(ctx context.Context, crewID, userID uuid.UUID) (crews.Role, error) {
	if r.memberFails.Load() {
		return "", errors.New("the database is having a moment")
	}
	return r.ChatRepository.MemberRole(ctx, crewID, userID)
}

type movableClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *movableClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *movableClock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// gatedServer is a live server whose chat goes through gateRepo and clock.
func gatedServer(t *testing.T, r room) (liveServer, *gateRepo, *movableClock) {
	t.Helper()
	repo := &gateRepo{ChatRepository: crews.NewRepository(r.pool)}
	clock := &movableClock{now: time.Now()}
	chat := crews.NewChat(repo, r.mod, moderation.NewFilter(), jobs.NewQueue(r.pool)).WithClock(clock.Now)

	hub := live.NewHub()
	t.Cleanup(hub.Close)
	api := echox.New(echox.Config{})
	crews.NewAPI(r.svc, "https://north.test").WithLive(chat, hub).Register(api.Group("", echo.WrapMiddleware(asUser)))
	s := liveServer{hub: hub, api: httptest.NewServer(api)}
	t.Cleanup(s.api.Close)
	return s, repo, clock
}

func (s liveServer) publishMessage(r room, m crews.Message, author users.User) {
	s.hub.Publish(live.Event{
		Type: live.EventMessage, CrewID: r.crew.ID, ChannelID: r.general,
		MessageID: m.ID, Seq: m.Seq, AuthorID: author.ID,
	})
}

func (s *stream) closes(t *testing.T) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case f, ok := <-s.frames:
			if !ok {
				return
			}
			if f != "ping" {
				t.Fatalf("frame %q; want the stream closed", f)
			}
		case <-deadline:
			t.Fatal("stream still open after 2s")
		}
	}
}

// The defect. A removal can be lost in a listener reconnect gap or to overflow,
// and then only a resync arrives. Someone no longer in the crew must not keep
// receiving ids and cursors until they happen to disconnect.
func TestAResyncEndsTheStreamOfSomeoneNoLongerInTheCrew(t *testing.T) {
	r := newRoom(t)
	s := newLiveServer(t, r)
	_, anas := open(t, s.api.URL, "/crews/"+r.crew.ID.String()+"/live", r.ana)
	anas.subscribed(t)

	// Removed in the database. Nothing listens in this test, so the removed event
	// is lost, exactly as it is in a reconnect gap.
	if err := r.svc.Remove(context.Background(), r.crew.ID, r.owner.ID, r.ana.ID); err != nil {
		t.Fatal(err)
	}
	s.hub.Resync()

	data := anas.expect(t, "removed")
	if !strings.Contains(data, `"userId":"`+r.ana.ID.String()+`"`) {
		t.Errorf("removed frame %s does not name the viewer", data)
	}
	anas.closes(t)
}

func TestAResyncKeepsAMembersStreamOpen(t *testing.T) {
	r := newRoom(t)
	s := newLiveServer(t, r)
	_, anas := open(t, s.api.URL, "/crews/"+r.crew.ID.String()+"/live", r.ana)
	_, owners := open(t, s.api.URL, "/crews/"+r.crew.ID.String()+"/live", r.owner)
	anas.subscribed(t)
	owners.subscribed(t)

	s.hub.Resync()
	anas.expect(t, "resync")
	owners.expect(t, "resync") // The owner has no member row, and is still in.

	m := r.send(t, r.leo, "still here")
	s.publishMessage(r, m, r.leo)
	anas.expect(t, "message")
}

// A resync is what a listener reconnect sends, which is when Postgres is most
// likely to be failing. Ending every stream in the process over that would
// stampede it with reconnects, so a failed check waits for the next event.
func TestAFailedMembershipCheckKeepsTheStreamAndAsksAgain(t *testing.T) {
	r := newRoom(t)
	s, repo, _ := gatedServer(t, r)
	_, anas := open(t, s.api.URL, "/crews/"+r.crew.ID.String()+"/live", r.ana)
	anas.subscribed(t)

	if err := r.svc.Remove(context.Background(), r.crew.ID, r.owner.ID, r.ana.ID); err != nil {
		t.Fatal(err)
	}

	repo.memberFails.Store(true)
	s.hub.Resync()
	anas.expect(t, "resync")

	repo.memberFails.Store(false)
	s.hub.Publish(live.Event{Type: live.EventChannels, CrewID: r.crew.ID})
	anas.expect(t, "removed")
	anas.closes(t)
}

// The saving: a busy room costs each stream one list, not one query per event.
func TestAStreamChecksBlocksWithoutAQueryPerEvent(t *testing.T) {
	r := newRoom(t)
	s, repo, _ := gatedServer(t, r)
	_, anas := open(t, s.api.URL, "/crews/"+r.crew.ID.String()+"/live", r.ana)
	anas.subscribed(t)

	m := r.send(t, r.leo, "one of many")
	for range 5 {
		s.publishMessage(r, m, r.leo)
		anas.expect(t, "message")
	}

	if got := repo.authorChecks.Load(); got != 0 {
		t.Errorf("%d per-event author queries, want 0", got)
	}
	if got := repo.blockLists.Load(); got != 1 {
		t.Errorf("%d block list loads, want 1", got)
	}
}

func TestABlockInEitherDirectionDropsTheAuthorsEvents(t *testing.T) {
	cases := []struct {
		name string
		by   func(room) (blocker, blocked uuid.UUID)
	}{
		{"the viewer blocked the author", func(r room) (uuid.UUID, uuid.UUID) { return r.ana.ID, r.leo.ID }},
		{"the author blocked the viewer", func(r room) (uuid.UUID, uuid.UUID) { return r.leo.ID, r.ana.ID }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRoom(t)
			s, _, _ := gatedServer(t, r)
			m := r.send(t, r.leo, "from leo")

			blocker, blocked := tc.by(r)
			if err := social.NewRepository(r.pool).Block(context.Background(), blocker, blocked); err != nil {
				t.Fatal(err)
			}

			_, anas := open(t, s.api.URL, "/crews/"+r.crew.ID.String()+"/live", r.ana)
			anas.subscribed(t)
			s.publishMessage(r, m, r.leo)
			anas.quiet(t)
		})
	}
}

// Blocking somebody mid-stream has to take effect without a reconnect. The list
// is a copy, so it is only allowed to be stale briefly.
func TestABlockReachesAnOpenStreamWhenTheListRefreshes(t *testing.T) {
	r := newRoom(t)
	s, _, clock := gatedServer(t, r)
	_, anas := open(t, s.api.URL, "/crews/"+r.crew.ID.String()+"/live", r.ana)
	anas.subscribed(t)

	m := r.send(t, r.leo, "from leo")
	s.publishMessage(r, m, r.leo)
	anas.expect(t, "message")

	if err := social.NewRepository(r.pool).Block(context.Background(), r.ana.ID, r.leo.ID); err != nil {
		t.Fatal(err)
	}

	clock.Add(crews.BlockListRefresh + time.Second)
	s.publishMessage(r, m, r.leo)
	anas.quiet(t)
}

func TestAResyncRefreshesTheBlockList(t *testing.T) {
	r := newRoom(t)
	s, _, _ := gatedServer(t, r)
	_, anas := open(t, s.api.URL, "/crews/"+r.crew.ID.String()+"/live", r.ana)
	anas.subscribed(t)

	m := r.send(t, r.leo, "from leo")
	s.publishMessage(r, m, r.leo)
	anas.expect(t, "message")

	if err := social.NewRepository(r.pool).Block(context.Background(), r.ana.ID, r.leo.ID); err != nil {
		t.Fatal(err)
	}

	s.hub.Resync()
	anas.expect(t, "resync")
	s.publishMessage(r, m, r.leo)
	anas.quiet(t)
}

// An event only carries the author so that each stream can apply its own block
// list. Clients are told a message exists, not who wrote it, and a blocked
// person's id must not start travelling to them.
func TestFramesDoNotCarryTheAuthor(t *testing.T) {
	r := newRoom(t)
	s, _, _ := gatedServer(t, r)
	_, anas := open(t, s.api.URL, "/crews/"+r.crew.ID.String()+"/live", r.ana)
	anas.subscribed(t)

	m := r.send(t, r.leo, "from leo")
	s.publishMessage(r, m, r.leo)

	data := anas.expect(t, "message")
	if strings.Contains(data, "authorId") || strings.Contains(data, r.leo.ID.String()) {
		t.Errorf("frame carries the author: %s", data)
	}
}

// Writers have to fill the author in, or every stream falls back to one query
// per event and the cache buys nothing.
func TestEveryMessageWriteNamesItsAuthorInTheEvent(t *testing.T) {
	r := newRoom(t)
	ctx := context.Background()

	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err = conn.Exec(ctx, "LISTEN "+live.Channel); err != nil {
		t.Fatal(err)
	}

	next := func(wantType string) live.Event {
		t.Helper()
		waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		n, waitErr := conn.Conn().WaitForNotification(waitCtx)
		if waitErr != nil {
			t.Fatalf("waiting for a %s event: %v", wantType, waitErr)
		}
		var ev live.Event
		if decodeErr := json.Unmarshal([]byte(n.Payload), &ev); decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if ev.Type != wantType {
			t.Fatalf("event type %q, want %q", ev.Type, wantType)
		}
		return ev
	}

	m := r.send(t, r.leo, "from leo")
	if ev := next(live.EventMessage); ev.AuthorID != r.leo.ID {
		t.Errorf("new message: author %s, want leo", ev.AuthorID)
	}

	if err = r.chat.Delete(ctx, r.crew.ID, m.ID, r.leo.ID); err != nil {
		t.Fatal(err)
	}
	if ev := next(live.EventUpdate); ev.AuthorID != r.leo.ID {
		t.Errorf("delete: author %s, want leo", ev.AuthorID)
	}

	// Last, because a hidden message can no longer be acted on by its author.
	other := r.send(t, r.leo, "from leo again")
	next(live.EventMessage)
	if err = r.chat.Hider().Hide(ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	if ev := next(live.EventUpdate); ev.AuthorID != r.leo.ID {
		t.Errorf("hide: author %s, want leo", ev.AuthorID)
	}
}
