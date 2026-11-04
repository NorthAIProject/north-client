package messaging

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

// seenUpdateRetention is how long a claimed update is remembered. Telegram keeps
// an undelivered update for 24 hours, so two days outlasts any redelivery with
// room for a slow or failed sweep.
const seenUpdateRetention = 48 * time.Hour

// UpdateSweeper forgets claimed updates once no platform can resend them.
type UpdateSweeper struct {
	links *PostgresRepository
	log   *slog.Logger
}

// NewUpdateSweeper returns a sweeper over links.
func NewUpdateSweeper(links *PostgresRepository, log *slog.Logger) *UpdateSweeper {
	return &UpdateSweeper{links: links, log: log}
}

// HandleSweep is registered against jobs.KindSweepMessagingUpdates and enqueued
// periodically, so it goes through the same retry and timeout machinery as every
// other sweep. Logged even when nothing went, because a sweep that stopped
// running looks exactly like one with nothing to do.
func (s *UpdateSweeper) HandleSweep(ctx context.Context, _ json.RawMessage) error {
	purged, err := s.links.PurgeSeenUpdates(ctx, seenUpdateRetention)
	if err != nil {
		return err
	}
	s.log.Info("swept seen messaging updates", slog.Int64("purged", purged))
	return nil
}
