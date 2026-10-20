package live_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/crews/live"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
)

func TestListenDeliversAndResyncsAfterReconnect(t *testing.T) {
	pool := testdb.New(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hub := live.NewHub()
	crew := uuid.New()
	ch, unsubscribe := hub.Subscribe(crew)
	defer unsubscribe()

	done := make(chan error, 1)
	go func() {
		done <- live.Listen(ctx, pool.Config().ConnConfig.ConnString(), hub, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()

	notify := func(seq int64) {
		t.Helper()
		if err := live.Notify(ctx, pool, live.Event{Type: live.EventMessage, CrewID: crew, Seq: seq}); err != nil {
			t.Fatal(err)
		}
	}
	// The listener needs a moment to LISTEN; keep notifying until it hears.
	waitFor := func(want func(live.Event) bool, poke func(), within time.Duration) {
		t.Helper()
		deadline := time.After(within)
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case ev := <-ch:
				if want(ev) {
					return
				}
			case <-tick.C:
				if poke != nil {
					poke()
				}
			case <-deadline:
				t.Fatal("event never arrived")
			}
		}
	}
	waitFor(func(ev live.Event) bool { return ev.Seq == 1 }, func() { notify(1) }, 2*time.Second)

	if _, err := pool.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity
		WHERE query LIKE 'LISTEN%' AND pid <> pg_backend_pid()`); err != nil {
		t.Fatal(err)
	}
	waitFor(func(ev live.Event) bool { return ev.Type == live.EventResync }, nil, 5*time.Second)
	waitFor(func(ev live.Event) bool { return ev.Seq == 2 }, func() { notify(2) }, 2*time.Second)

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Listen returned %v after cancel", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Listen did not return after cancel")
	}
}
