// Package jobs is North's background work queue.
//
// It is a Postgres table plus SELECT ... FOR UPDATE SKIP LOCKED. That is a
// real queue: concurrent workers step over each other's locked rows rather than
// blocking, jobs survive a restart, and retries are a timestamp rather than a
// sleeping goroutine.
//
// No queue dependency, deliberately. Redis or a broker would add an operational
// component, a failure mode, and a second source of truth, to do something the
// database North already runs does correctly. CLAUDE.md asks whether the
// standard library and existing pieces can solve it first; here they can.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	jobsdb "github.com/NorthAIProject/north-client/internal/jobs/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Kind identifies what a job does.
type Kind string

const (
	// KindAnalyzeFormVideo runs a form analysis over an uploaded video.
	KindAnalyzeFormVideo Kind = "analyze_form_video"

	// KindExtractMemories proposes durable profile facts from a conversation.
	KindExtractMemories Kind = "extract_memories"

	// KindSyncStrava imports recent activities from a linked Strava account.
	//
	// Enqueued when someone connects and when they ask for a sync — never on
	// a timer, because North has no scheduler.
	KindSyncStrava Kind = "sync_strava"
)

// ExtractMemoriesPayload is the job body for KindExtractMemories.
//
// Lives here (not in memories) so coach can enqueue without an import cycle:
// memories imports coach for ContextSource; coach must not import memories.
type ExtractMemoriesPayload struct {
	UserID         uuid.UUID `json:"user_id"`
	ConversationID uuid.UUID `json:"conversation_id"`
}

// SyncStravaPayload is the job body for KindSyncStrava. Here for the same
// reason as above: the enqueueing side must not have to import the package
// that handles it.
type SyncStravaPayload struct {
	UserID uuid.UUID `json:"user_id"`
}

// Job is a unit of queued work.
type Job struct {
	ID          uuid.UUID
	Kind        Kind
	Payload     json.RawMessage
	Attempts    int
	MaxAttempts int
	LastError   string
	CreatedAt   time.Time
}

// Exhausted reports whether this job has used its last attempt.
func (j Job) Exhausted() bool { return j.Attempts >= j.MaxAttempts }

// Queue enqueues and claims jobs.
type Queue struct {
	q *jobsdb.Queries
}

func NewQueue(pool *pgxpool.Pool) *Queue {
	return &Queue{q: jobsdb.New(pool)}
}

// Enqueue adds a job. payload is marshalled to JSON.
func (q *Queue) Enqueue(ctx context.Context, kind Kind, payload any) (Job, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return Job{}, apperr.Wrap(err, "encode job payload")
	}

	row, err := q.q.EnqueueJob(ctx, jobsdb.EnqueueJobParams{
		Kind:        string(kind),
		Payload:     body,
		MaxAttempts: 3,
	})
	if err != nil {
		return Job{}, apperr.Wrap(err, "enqueue %s", kind)
	}
	return fromDB(row), nil
}

// Claim takes one eligible job, or returns false when the queue is empty.
func (q *Queue) Claim(ctx context.Context) (Job, bool, error) {
	row, err := q.q.ClaimJob(ctx)
	if err != nil {
		// An empty queue is the common case, not an error.
		if errors.Is(err, pgx.ErrNoRows) {
			return Job{}, false, nil
		}
		return Job{}, false, apperr.Wrap(err, "claim job")
	}
	return fromDB(row), true, nil
}

// Complete marks a job done.
func (q *Queue) Complete(ctx context.Context, id uuid.UUID) error {
	return apperr.Wrap(q.q.CompleteJob(ctx, id), "complete job")
}

// Retry returns a job to the queue after a delay.
func (q *Queue) Retry(ctx context.Context, id uuid.UUID, after time.Time, reason string) error {
	return apperr.Wrap(q.q.RetryJob(ctx, jobsdb.RetryJobParams{
		ID:        id,
		RunAfter:  after,
		LastError: reason,
	}), "retry job")
}

// Fail marks a job permanently failed.
func (q *Queue) Fail(ctx context.Context, id uuid.UUID, reason string) error {
	return apperr.Wrap(q.q.FailJob(ctx, jobsdb.FailJobParams{ID: id, LastError: reason}), "fail job")
}

// ReleaseStale returns jobs abandoned by a worker that died mid-run.
//
// Without this a crash leaves them 'running' forever and the work silently
// never happens — which for a form analysis means a user waiting on a spinner
// that will never resolve.
func (q *Queue) ReleaseStale(ctx context.Context, olderThan time.Duration) (int64, error) {
	n, err := q.q.ReleaseStaleJobs(ctx, olderThan.Seconds())
	return n, apperr.Wrap(err, "release stale jobs")
}

// Backoff is the delay before a job's next attempt.
//
// Exponential, so a provider having a bad minute is not hammered, and a job
// whose input is genuinely broken burns its attempts over minutes rather than
// milliseconds.
func Backoff(attempt int) time.Duration {
	switch {
	case attempt <= 1:
		return 30 * time.Second
	case attempt == 2:
		return 2 * time.Minute
	default:
		return 10 * time.Minute
	}
}

func fromDB(row jobsdb.Job) Job {
	j := Job{
		ID:          row.ID,
		Kind:        Kind(row.Kind),
		Payload:     row.Payload,
		Attempts:    int(row.Attempts),
		MaxAttempts: int(row.MaxAttempts),
		LastError:   row.LastError,
		CreatedAt:   row.CreatedAt,
	}
	return j
}
