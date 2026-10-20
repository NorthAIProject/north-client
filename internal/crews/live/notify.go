// Package live carries crew events from whoever wrote to whoever is watching:
// writers notify inside their transaction, one listener per web process
// hears them, and a hub fans them out to open streams.
//
// An event is only a signal, ids and a cursor. Clients fetch what changed
// themselves, so blocks, hides and language are applied per viewer.
package live

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Channel is the Postgres NOTIFY channel crew events travel on.
const Channel = "crew_events"

// Event types.
const (
	EventMessage  = "message"  // a new message; Seq is its cursor
	EventUpdate   = "update"   // a message was edited, deleted, reacted to, pinned or hidden
	EventChannels = "channels" // the channel list or the member list changed
	EventRemoved  = "removed"  // UserID left or was removed
	EventResync   = "resync"   // something may have been missed: refetch
)

type Event struct {
	Type      string    `json:"type"`
	CrewID    uuid.UUID `json:"crewId"`
	ChannelID uuid.UUID `json:"channelId,omitzero"`
	MessageID uuid.UUID `json:"messageId,omitzero"`
	UserID    uuid.UUID `json:"userId,omitzero"`
	Seq       int64     `json:"seq,omitempty"`
}

// Execer is a transaction, or a pool for an event that follows a write
// already committed.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Notify queues ev on db. Inside a transaction Postgres delivers it when the
// transaction commits and drops it on rollback, so nobody hears about a
// write that did not happen.
func Notify(ctx context.Context, db Execer, ev Event) error {
	body, err := json.Marshal(ev)
	if err != nil {
		return apperr.Wrap(err, "encode crew event")
	}
	if _, err = db.Exec(ctx, "SELECT pg_notify($1, $2)", Channel, string(body)); err != nil {
		return apperr.Wrap(err, "notify crew event")
	}
	return nil
}
