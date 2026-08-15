package quota

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

// retention is how long a closed window is kept before the sweep drops it.
//
// A day rather than an hour, so an operator investigating a complaint about a
// refusal can still see the rows that caused it. Nothing reads them, so the
// only cost of keeping them a while is the space.
const retention = 24 * time.Hour

// HandleSweep drops rate-limit windows that have already closed.
//
// Registered against jobs.KindSweepQuotas and enqueued periodically. It takes
// the job signature rather than being a method the worker calls directly so it
// goes through the same retry and timeout machinery as everything else.
func (s *Service) HandleSweep(ctx context.Context, _ json.RawMessage) error {
	before := s.now().Add(-retention)

	if err := s.counter.Sweep(ctx, before); err != nil {
		return err
	}

	s.log.Info("swept closed quota windows", slog.Time("before", before))
	return nil
}
