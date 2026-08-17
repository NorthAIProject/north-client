package conversations

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/jobs"
)

// summarySweepBatch bounds one pass of the sweep.
const summarySweepBatch = 50

// Enqueuer is the sweep's view of the job queue. *jobs.Queue satisfies it.
type Enqueuer interface {
	Enqueue(ctx context.Context, kind jobs.Kind, payload any) (jobs.Job, error)
	HasPendingJobForUser(ctx context.Context, kind jobs.Kind, userID uuid.UUID) (bool, error)
}

// SummarySweeper finds threads that outgrew the context window without the
// coach's own trigger firing.
//
// The trigger runs in the reply pump. A conversation continued from Telegram or
// MCP, or one whose enqueue was lost to a restart, is invisible to it — which is
// what this exists to catch, exactly as the memory sweep catches quiet threads.
type SummarySweeper struct {
	convos     *Service
	queue      Enqueuer
	keepRecent int
	log        *slog.Logger
}

func NewSummarySweeper(convos *Service, queue Enqueuer, keepRecent int, log *slog.Logger) *SummarySweeper {
	if keepRecent <= 0 {
		keepRecent = DefaultKeepRecent
	}
	if log == nil {
		log = slog.Default()
	}
	return &SummarySweeper{convos: convos, queue: queue, keepRecent: keepRecent, log: log}
}

func (s *SummarySweeper) HandleSweep(ctx context.Context, _ json.RawMessage) error {
	if s.queue == nil || s.convos == nil {
		return nil
	}

	pending, err := s.convos.AwaitingSummary(ctx, s.keepRecent, summarySweepBatch)
	if err != nil {
		return err
	}

	enqueued := 0
	for _, c := range pending {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// One compaction per person per pass, for the reason the memory sweep
		// rations itself: a backlog of long threads must not become a burst of
		// model calls, and a job still waiting from last time is not doubled.
		has, err := s.queue.HasPendingJobForUser(ctx, jobs.KindSummarizeConversation, c.UserID)
		if err != nil {
			return err
		}
		if has {
			continue
		}

		if _, err := s.queue.Enqueue(ctx, jobs.KindSummarizeConversation, jobs.SummarizeConversationPayload{
			UserID:         c.UserID,
			ConversationID: c.ID,
		}); err != nil {
			s.log.Warn("could not enqueue conversation summary",
				slog.String("conversation_id", c.ID.String()), slog.Any("error", err))
			continue
		}
		enqueued++
	}

	if enqueued > 0 {
		s.log.Info("sweep started conversation summaries", slog.Int("started", enqueued))
	}
	return nil
}
