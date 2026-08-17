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
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
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

	// KindIndexDocument parses and chunks one uploaded document or note.
	KindIndexDocument Kind = "index_document"

	// KindReindexUser rebuilds every chunk one person has.
	//
	// The schema calls chunks derived state. A claim like that is only true
	// while something exercises it, and this is that something.
	KindReindexUser Kind = "reindex_user"

	// KindEmbedChunks computes vectors for passages that have none.
	//
	// Its own job, after indexing, because it fails differently: chunking is
	// local and deterministic, embedding is a call to somebody else's service.
	// A passage must be searchable by text before that call succeeds.
	KindEmbedChunks Kind = "embed_chunks"

	// KindSweepEmbeddings enqueues embed work for users with missing vectors.
	KindSweepEmbeddings Kind = "sweep_embeddings"

	// KindSyncVault imports files from a connected local vault folder.
	KindSyncVault Kind = "sync_vault"

	// KindWeeklyReview writes one week's review from the person's recorded week.
	KindWeeklyReview Kind = "weekly_review"

	// KindSweepMemories enqueues extraction for conversations that went quiet
	// before the coach's own trigger fired.
	//
	// That trigger only runs at four messages, in the reply pump. A thread that
	// says something worth remembering and then stops at three is invisible to
	// it, which is what this sweep exists to catch.
	KindSweepMemories Kind = "sweep_memories"

	// KindSweepQuotas drops rate-limit windows that have already closed.
	//
	// Nothing reads a closed window — a request always lands in the current one
	// — so this is purely reclaiming space. Without it the table grows one row
	// per account per guarded action per window, forever.
	KindSweepQuotas Kind = "sweep_quotas"

	// KindSweepNudges evaluates missed check-ins and approaching deadlines
	// for onboarded accounts and stores any new in-app notes.
	KindSweepNudges Kind = "sweep_nudges"

	// KindSweepReports enqueues the weekly review for accounts that asked for
	// it, once their own week has closed. Hourly, because "their own week"
	// closes at a different absolute time in every timezone.
	KindSweepReports Kind = "sweep_reports"

	// KindDailyBriefing writes one morning's briefing. Its own kind rather than
	// a flag on KindWeeklyReview so the two can be retried and counted apart:
	// a briefing is cheap and disposable, a review is neither.
	KindDailyBriefing Kind = "daily_briefing"

	// KindSweepBriefings enqueues the daily briefing for accounts that asked
	// for it, once their own morning has arrived. Hourly, for the same reason
	// KindSweepReports is: "their own morning" is a different absolute time in
	// every timezone.
	KindSweepBriefings Kind = "sweep_briefings"

	// KindSummarizeConversation compacts the older turns of one long thread so
	// its beginning survives past the context window.
	KindSummarizeConversation Kind = "summarize_conversation"

	// KindSweepSummaries enqueues compaction for threads that outgrew the
	// context window without the coach's own trigger firing — a conversation
	// resumed from Telegram, or one whose enqueue was lost.
	KindSweepSummaries Kind = "sweep_summaries"
)

// SummarizeConversationPayload is the job body for KindSummarizeConversation.
//
// UserID is carried even though the handler re-reads the owner from the row:
// HasPendingJobForUser matches on payload->>'user_id', so a payload without it
// would make the "one compaction per person per pass" rationing silently never
// match anything.
type SummarizeConversationPayload struct {
	UserID         uuid.UUID `json:"user_id"`
	ConversationID uuid.UUID `json:"conversation_id"`
}

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

// IndexDocumentPayload and ReindexUserPayload are the job bodies for the
// knowledge index, here for the same reason as the two above.
type IndexDocumentPayload struct {
	UserID     uuid.UUID `json:"user_id"`
	DocumentID uuid.UUID `json:"document_id"`
}

type ReindexUserPayload struct {
	UserID uuid.UUID `json:"user_id"`
}

type EmbedChunksPayload struct {
	UserID uuid.UUID `json:"user_id"`
}

type SyncVaultPayload struct {
	UserID uuid.UUID `json:"user_id"`
}

// WeeklyReviewPayload is the job body for KindWeeklyReview.
type WeeklyReviewPayload struct {
	UserID   uuid.UUID `json:"user_id"`
	ReportID uuid.UUID `json:"report_id"`
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

	// RequestID is the request that queued this job, empty when nothing did —
	// the worker's own periodic sweeps. Carried so a failure hours later can be
	// joined to whatever a person was doing at the time.
	RequestID string
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
		// Read from the context rather than passed in, so every caller is
		// covered without remembering to: eight payload types and one Enqueue.
		RequestID: nilIfEmpty(middleware.RequestIDFrom(ctx)),
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

// RequeueFailedEmbedJobsForUser returns permanently failed embed jobs to pending.
func (q *Queue) RequeueFailedEmbedJobsForUser(ctx context.Context, userID uuid.UUID) (int64, error) {
	n, err := q.q.RequeueFailedEmbedJobsForUser(ctx, userID)
	return n, apperr.Wrap(err, "requeue failed embed jobs")
}

// HasPendingJobForUser reports whether a job kind is already queued for a user.
func (q *Queue) HasPendingJobForUser(ctx context.Context, kind Kind, userID uuid.UUID) (bool, error) {
	ok, err := q.q.HasPendingJobForUser(ctx, jobsdb.HasPendingJobForUserParams{
		Kind:   string(kind),
		UserID: userID,
	})
	return ok, apperr.Wrap(err, "check pending job")
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
	if row.RequestID != nil {
		j.RequestID = *row.RequestID
	}
	return j
}

// nilIfEmpty keeps an absent request id out of the column as NULL rather than
// as an empty string, so "no request queued this" and "the id was lost" stay
// distinguishable in the data.
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
