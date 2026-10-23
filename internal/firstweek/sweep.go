package firstweek

import (
	"context"
	"encoding/json"
	"log/slog"
)

// Sweeper is the worker's hourly visit to every running week. Completion is
// recorded once per day by the primary key, so a repeated hour, or a second
// worker, is harmless.
type Sweeper struct {
	svc *Service
}

func NewSweeper(svc *Service) *Sweeper { return &Sweeper{svc: svc} }

func (sw *Sweeper) HandleSweep(ctx context.Context, _ json.RawMessage) error {
	if sw.svc == nil {
		return nil
	}
	ended, err := sw.svc.Sweep(ctx)
	if ended > 0 {
		sw.svc.log.Info("first weeks abandoned", slog.Int("count", ended))
	}
	return err
}
