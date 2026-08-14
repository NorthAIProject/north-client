package memories

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/jobs"
)

// idleFor is how long a thread must be quiet before it counts as finished.
//
// Long enough that somebody who steps away mid-conversation and comes back is
// not extracted halfway through, short enough that a thread from this morning
// is filed by tonight.
const idleFor = 6 * time.Hour

// minSweepMessages is the shortest exchange worth reading.
//
// Two: one thing said and one answer. The coach's own trigger waits for four,
// which is the gap this sweep exists to close — the threads that stopped before
// it ever fired.
const minSweepMessages = 2

// sweepBatch bounds one pass. A backlog is worked through over several sweeps
// rather than enqueuing thousands of AI calls at once.
const sweepBatch = 50

// Enqueuer schedules extraction. *jobs.Queue satisfies it.
//
// HasPendingJobForUser is part of the contract because a sweep that ran again
// before the last batch drained would otherwise queue the same conversations a
// second time — each one an AI call.
type Enqueuer interface {
	Enqueue(ctx context.Context, kind jobs.Kind, payload any) (jobs.Job, error)
	HasPendingJobForUser(ctx context.Context, kind jobs.Kind, userID uuid.UUID) (bool, error)
}

// ExtractionSweeper finds conversations the coach's own trigger never reached.
//
// Follows documents.EmbedSweeper: a periodic job that only enqueues work, so
// the expensive part stays in the job that already knows how to do it and can
// be retried on its own.
type ExtractionSweeper struct {
	conversations *conversations.Service
	queue         Enqueuer
	log           *slog.Logger
	now           func() time.Time
}

func NewExtractionSweeper(convos *conversations.Service, queue Enqueuer, log *slog.Logger) *ExtractionSweeper {
	if log == nil {
		log = slog.Default()
	}
	return &ExtractionSweeper{conversations: convos, queue: queue, log: log, now: time.Now}
}

// WithClock fixes the sweeper's idea of now, so a test can make a thread old
// without waiting six hours.
func (s *ExtractionSweeper) WithClock(now func() time.Time) *ExtractionSweeper {
	s.now = now
	return s
}

func (s *ExtractionSweeper) HandleSweep(ctx context.Context, _ json.RawMessage) error {
	if s.queue == nil || s.conversations == nil {
		return nil
	}

	pending, err := s.conversations.AwaitingExtraction(
		ctx, s.now().Add(-idleFor), minSweepMessages, sweepBatch)
	if err != nil {
		return err
	}

	enqueued := 0
	for _, c := range pending {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// One extraction per person per pass. Somebody with a backlog of quiet
		// threads gets through it over several sweeps rather than in one burst
		// of AI calls, and a job still waiting from last time is not doubled.
		pending, err := s.queue.HasPendingJobForUser(ctx, jobs.KindExtractMemories, c.UserID)
		if err != nil {
			return err
		}
		if pending {
			continue
		}

		if _, err := s.queue.Enqueue(ctx, jobs.KindExtractMemories, jobs.ExtractMemoriesPayload{
			UserID:         c.UserID,
			ConversationID: c.ID,
		}); err != nil {
			return err
		}
		enqueued++
	}

	if enqueued > 0 {
		s.log.Info("sweep enqueued memory extraction", slog.Int("conversations", enqueued))
	}
	return nil
}
