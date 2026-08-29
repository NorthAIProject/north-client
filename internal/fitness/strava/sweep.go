package strava

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/jobs"
)

const (
	// sweepBatch bounds one pass, for the reason every other sweep in the
	// codebase is bounded: a backlog must drain over several passes rather
	// than becoming one burst against somebody else's API.
	sweepBatch = 50

	// defaultStaleAfter is how long a connection may go unsynced before the
	// sweep picks it up.
	//
	// Six hours rather than one. Strava allows 100 reads per fifteen minutes
	// across the whole application, a sync spends one of them per athlete, and
	// nothing downstream — the weekly training picture, the coach's context —
	// reads at an hourly resolution. Four passes a day per athlete is the
	// cheapest cadence that still makes "my run is in North" true the same
	// day without anyone clicking.
	defaultStaleAfter = 6 * time.Hour
)

// SweepEnqueuer is the sweep's view of the queue. *jobs.Queue satisfies it.
//
// Wider than Enqueuer by one method: the sweep must be able to tell that a
// user already has a sync waiting. Without that check a worker that is down,
// or simply slower than the sweep, accumulates one duplicate job per pass —
// which is precisely how four identical sync_strava rows came to sit in the
// queue while nothing ran them.
type SweepEnqueuer interface {
	Enqueue(ctx context.Context, kind jobs.Kind, payload any) (jobs.Job, error)
	HasPendingJobForUser(ctx context.Context, kind jobs.Kind, userID uuid.UUID) (bool, error)
}

// Sweeper enqueues a sync for every connection that has gone stale.
type Sweeper struct {
	repo       *Repository
	queue      SweepEnqueuer
	staleAfter time.Duration
	log        *slog.Logger
}

func NewSweeper(repo *Repository, queue SweepEnqueuer, staleAfter time.Duration, log *slog.Logger) *Sweeper {
	if staleAfter <= 0 {
		staleAfter = defaultStaleAfter
	}
	if log == nil {
		log = slog.Default()
	}
	return &Sweeper{repo: repo, queue: queue, staleAfter: staleAfter, log: log}
}

// HandleSweep is the worker entry point for jobs.KindSweepStrava.
func (s *Sweeper) HandleSweep(ctx context.Context, _ json.RawMessage) error {
	if s.repo == nil || s.queue == nil {
		return nil
	}

	due, err := s.repo.DueForSync(ctx, time.Now().Add(-s.staleAfter), sweepBatch)
	if err != nil {
		return err
	}

	enqueued := 0
	for _, userID := range due {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		has, err := s.queue.HasPendingJobForUser(ctx, jobs.KindSyncStrava, userID)
		if err != nil {
			return err
		}
		if has {
			continue
		}

		if _, err := s.queue.Enqueue(ctx, jobs.KindSyncStrava, jobs.SyncStravaPayload{UserID: userID}); err != nil {
			// One account's enqueue failing should not abandon the rest of the
			// batch: the next pass retries it in six hours at worst.
			s.log.Warn("could not enqueue strava sync",
				slog.Any("error", err), slog.String("user_id", userID.String()))
			continue
		}
		enqueued++
	}

	if enqueued > 0 {
		s.log.Info("sweep started strava syncs", slog.Int("started", enqueued))
	}
	return nil
}
