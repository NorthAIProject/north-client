package news

import (
	"context"
	"encoding/json"
	"log/slog"
)

// Sweeper is the worker-side entry point for KindSweepNewsTicker.
type Sweeper struct {
	svc *Service
	log *slog.Logger
}

func NewSweeper(svc *Service, log *slog.Logger) *Sweeper {
	return &Sweeper{svc: svc, log: log}
}

// HandleSweep runs one sweep. The payload is empty; the periodic enqueue
// carries nothing because there is nothing to parameterise.
func (s *Sweeper) HandleSweep(ctx context.Context, _ json.RawMessage) error {
	if err := s.svc.Sweep(ctx); err != nil {
		s.log.Warn("news ticker sweep failed", slog.Any("error", err))
		return err
	}
	return nil
}
