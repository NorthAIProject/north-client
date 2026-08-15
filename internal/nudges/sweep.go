package nudges

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
)

const sweepPageSize = 100

// Sweeper pages onboarded accounts and evaluates the nudge rules for each.
type Sweeper struct {
	svc *Service
	log *slog.Logger
}

func NewSweeper(svc *Service, log *slog.Logger) *Sweeper {
	if log == nil {
		log = slog.Default()
	}
	return &Sweeper{svc: svc, log: log}
}

func (s *Sweeper) HandleSweep(ctx context.Context, _ json.RawMessage) error {
	if s.svc == nil {
		return nil
	}

	var after uuid.UUID
	created := 0
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		page, err := s.svc.ListOnboarded(ctx, after, sweepPageSize)
		if err != nil {
			return err
		}
		if len(page) == 0 {
			break
		}

		for _, user := range page {
			n, err := s.svc.Evaluate(ctx, user)
			if err != nil {
				s.log.Error("nudge evaluate failed",
					slog.String("user_id", user.ID.String()),
					slog.Any("error", err),
				)
			} else {
				created += n
			}
			after = user.ID
		}

		if len(page) < sweepPageSize {
			break
		}
	}

	if created > 0 {
		s.log.Info("sweep created nudges", slog.Int("created", created))
	}
	return nil
}
