package strava_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/fitness/strava"
	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
)

// recordingQueue stands in for *jobs.Queue. The sweep's whole job is deciding
// what to enqueue, so what it enqueued is the only thing worth asserting.
type recordingQueue struct {
	enqueued []uuid.UUID
	pending  map[uuid.UUID]bool
}

func newRecordingQueue() *recordingQueue {
	return &recordingQueue{pending: map[uuid.UUID]bool{}}
}

func (q *recordingQueue) Enqueue(_ context.Context, _ jobs.Kind, payload any) (jobs.Job, error) {
	p, ok := payload.(jobs.SyncStravaPayload)
	if !ok {
		return jobs.Job{}, nil
	}
	q.enqueued = append(q.enqueued, p.UserID)
	return jobs.Job{}, nil
}

func (q *recordingQueue) HasPendingJobForUser(_ context.Context, _ jobs.Kind, userID uuid.UUID) (bool, error) {
	return q.pending[userID], nil
}

// A connection that has never synced is the first thing the sweep must catch:
// it is what a connect whose queued sync was lost looks like afterwards.
func TestTheSweepPicksUpAConnectionThatHasNeverSynced(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool, "never-synced@north.test")
	repo := strava.NewRepository(pool, sealer(t, 1))
	connect(t, repo, user.ID)

	queue := newRecordingQueue()
	if err := strava.NewSweeper(repo, queue, time.Hour, nil).HandleSweep(context.Background(), nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	if len(queue.enqueued) != 1 || queue.enqueued[0] != user.ID {
		t.Fatalf("enqueued = %v, want exactly [%v]", queue.enqueued, user.ID)
	}
}

func TestTheSweepLeavesAFreshlySyncedConnectionAlone(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool, "fresh@north.test")
	repo := strava.NewRepository(pool, sealer(t, 1))
	connect(t, repo, user.ID)

	if err := repo.MarkSynced(context.Background(), user.ID, time.Now()); err != nil {
		t.Fatalf("mark synced: %v", err)
	}

	queue := newRecordingQueue()
	if err := strava.NewSweeper(repo, queue, time.Hour, nil).HandleSweep(context.Background(), nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	if len(queue.enqueued) != 0 {
		t.Fatalf("enqueued = %v, want nothing", queue.enqueued)
	}
}

func TestTheSweepPicksUpAConnectionThatWentStale(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool, "stale@north.test")
	repo := strava.NewRepository(pool, sealer(t, 1))
	connect(t, repo, user.ID)

	if err := repo.MarkSynced(context.Background(), user.ID, time.Now().Add(-2*time.Hour)); err != nil {
		t.Fatalf("mark synced: %v", err)
	}

	queue := newRecordingQueue()
	if err := strava.NewSweeper(repo, queue, time.Hour, nil).HandleSweep(context.Background(), nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued = %v, want one sync", queue.enqueued)
	}
}

// The regression this sweep could otherwise cause. Four identical sync_strava
// rows once sat in the queue because nothing claimed them; an hourly sweep
// that ignored pending work would turn a stopped worker into an unbounded
// pile of duplicates.
func TestTheSweepDoesNotQueueASecondSyncWhileOneIsWaiting(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool, "already-queued@north.test")
	repo := strava.NewRepository(pool, sealer(t, 1))
	connect(t, repo, user.ID)

	queue := newRecordingQueue()
	queue.pending[user.ID] = true

	if err := strava.NewSweeper(repo, queue, time.Hour, nil).HandleSweep(context.Background(), nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	if len(queue.enqueued) != 0 {
		t.Fatalf("enqueued = %v, want nothing while a sync is already pending", queue.enqueued)
	}
}

// A failed sync must stay due. Advancing the watermark on failure would skip
// whatever that run never managed to fetch, permanently.
func TestAFailedSyncRemainsDueAndKeepsItsReason(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool, "failed@north.test")
	repo := strava.NewRepository(pool, sealer(t, 1))
	connect(t, repo, user.ID)
	ctx := context.Background()

	if err := repo.MarkSyncFailed(ctx, user.ID, "strava responded 401"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}

	conn, err := repo.Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if conn.LastSyncError != "strava responded 401" {
		t.Errorf("LastSyncError = %q, want the recorded reason", conn.LastSyncError)
	}
	if conn.LastSyncAttemptedAt == nil {
		t.Error("LastSyncAttemptedAt was not stamped")
	}
	if conn.LastSyncedAt != nil {
		t.Error("a failed sync moved the watermark")
	}

	queue := newRecordingQueue()
	if err := strava.NewSweeper(repo, queue, time.Hour, nil).HandleSweep(ctx, nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(queue.enqueued) != 1 {
		t.Fatalf("enqueued = %v, want the failed connection retried", queue.enqueued)
	}
}

// And success clears it again, so yesterday's failure does not outlive the
// sync that fixed it.
func TestASuccessfulSyncClearsTheRecordedFailure(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool, "recovered@north.test")
	repo := strava.NewRepository(pool, sealer(t, 1))
	connect(t, repo, user.ID)
	ctx := context.Background()

	if err := repo.MarkSyncFailed(ctx, user.ID, "strava responded 500"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	if err := repo.MarkSynced(ctx, user.ID, time.Now()); err != nil {
		t.Fatalf("mark synced: %v", err)
	}

	conn, err := repo.Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if conn.LastSyncError != "" {
		t.Errorf("LastSyncError = %q, want it cleared", conn.LastSyncError)
	}
	if conn.LastSyncedAt == nil {
		t.Error("LastSyncedAt was not stamped")
	}
}

// Sync is reachable from a process that has the connection but not the
// credentials — the worker deployment. Without this guard it reached a nil
// oauth config and panicked on the first token refresh.
func TestSyncRefusesWhenStravaIsNotConfigured(t *testing.T) {
	t.Parallel()

	svc := strava.NewService(strava.Options{})
	if _, err := svc.Sync(context.Background(), uuid.New()); err == nil {
		t.Fatal("Sync succeeded with no credentials configured")
	}
}
