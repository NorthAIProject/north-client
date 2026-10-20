package live

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
)

// Listen holds one connection that LISTENs for crew events and publishes
// them to hub, until ctx ends. A lost connection is retried with backoff
// from 1s to 30s, and every reconnect resyncs the hub, since anything sent
// while it was down was missed.
//
// It needs a session of its own: through a transaction pooler such as
// PgBouncer in transaction mode, LISTEN does not survive, so databaseURL
// must reach Postgres directly.
func Listen(ctx context.Context, databaseURL string, hub *Hub, log *slog.Logger) error {
	backoff := time.Second
	connectedBefore := false
	for {
		err := listenOnce(ctx, databaseURL, hub, log, func() {
			if connectedBefore {
				hub.Resync()
			}
			connectedBefore = true
			backoff = time.Second
		})
		if ctx.Err() != nil {
			return nil
		}
		log.Warn("crew live listener lost its connection", slog.Any("error", err), slog.Duration("retry_in", backoff))
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

func listenOnce(ctx context.Context, databaseURL string, hub *Hub, log *slog.Logger, connected func()) error {
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.Background()) }()
	if _, err = conn.Exec(ctx, "LISTEN "+Channel); err != nil {
		return err
	}
	log.Info("crew live listener connected")
	connected()
	for {
		n, waitErr := conn.WaitForNotification(ctx)
		if waitErr != nil {
			return waitErr
		}
		var ev Event
		if err = json.Unmarshal([]byte(n.Payload), &ev); err != nil {
			log.Warn("crew live listener skipped an unreadable event", slog.Any("error", err))
			continue
		}
		hub.Publish(ev)
	}
}
