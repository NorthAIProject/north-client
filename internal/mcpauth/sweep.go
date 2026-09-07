package mcpauth

import (
	"context"
	"encoding/json"
	"log/slog"
)

// HandleSweep removes what the connector leaves behind.
//
// Registered against jobs.KindSweepOAuth and enqueued periodically. It takes
// the job signature rather than being a method the worker calls directly, so
// it goes through the same retry and timeout machinery as every other sweep.
//
// Four things age out, and only one of them is really about space. Spent
// authorization requests, spent codes and dead refresh tokens are kept for a
// while past their expiry deliberately: an operator investigating "my agent
// stopped working" wants to see the row that refused, and nothing reads them
// otherwise. Unused client registrations are the load-bearing case —
// registration is open, and a native client registers a new row every launch
// because its callback port changes, so without this the table grows for as
// long as anybody uses the feature.
func (s *Service) HandleSweep(ctx context.Context, _ json.RawMessage) error {
	swept, err := s.Sweep(ctx)
	if err != nil {
		return err
	}

	// Logged even when everything is zero. A sweep that silently stopped
	// running looks exactly like a sweep with nothing to do, and the
	// difference matters for the one table that grows without it.
	s.log().Info("swept oauth rows",
		slog.Int64("requests", swept.Requests),
		slog.Int64("codes", swept.Codes),
		slog.Int64("refresh_tokens", swept.RefreshTokens),
		slog.Int64("unused_clients", swept.Clients))

	return nil
}

func (s *Service) log() *slog.Logger {
	if s.logger != nil {
		return s.logger
	}
	return slog.Default()
}
