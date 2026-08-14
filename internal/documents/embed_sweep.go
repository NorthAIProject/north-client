package documents

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/NorthAIProject/north-client/internal/jobs"
)

// EmbedSweeper enqueues embed jobs for users with missing vectors.
type EmbedSweeper struct {
	repo  *Repository
	queue *jobs.Queue
	model string
	log   *slog.Logger
}

func NewEmbedSweeper(repo *Repository, queue *jobs.Queue, model string, log *slog.Logger) *EmbedSweeper {
	if log == nil {
		log = slog.Default()
	}
	return &EmbedSweeper{repo: repo, queue: queue, model: model, log: log}
}

func (s *EmbedSweeper) HandleSweep(ctx context.Context, _ json.RawMessage) error {
	if s.queue == nil || s.model == "" {
		return nil
	}

	users, err := s.repo.UsersWithEmbeddingGap(ctx, s.model, 100)
	if err != nil {
		return err
	}

	enqueued := 0
	for _, userID := range users {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		pending, err := s.queue.HasPendingJobForUser(ctx, jobs.KindEmbedChunks, userID)
		if err != nil {
			return err
		}
		if pending {
			continue
		}

		if _, err := s.queue.Enqueue(ctx, jobs.KindEmbedChunks, jobs.EmbedChunksPayload{UserID: userID}); err != nil {
			return err
		}
		enqueued++
	}

	if enqueued > 0 {
		s.log.Info("sweep enqueued embed jobs", slog.Int("users", enqueued))
	}
	return nil
}
