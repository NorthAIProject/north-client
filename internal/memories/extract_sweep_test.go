package memories_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/memories"
	"github.com/NorthAIProject/north-client/internal/memories/extract"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
)

// recordingQueue keeps every enqueue so a test can see the same conversation
// being scheduled twice, which is the failure worth catching here.
type recordingQueue struct {
	payloads []jobs.ExtractMemoriesPayload
	pending  bool
}

func (q *recordingQueue) Enqueue(_ context.Context, kind jobs.Kind, payload any) (jobs.Job, error) {
	if p, ok := payload.(jobs.ExtractMemoriesPayload); ok {
		q.payloads = append(q.payloads, p)
	}
	return jobs.Job{ID: uuid.New(), Kind: kind}, nil
}

func (q *recordingQueue) HasPendingJobForUser(_ context.Context, _ jobs.Kind, _ uuid.UUID) (bool, error) {
	return q.pending, nil
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return raw
}

type stubExtractor struct {
	candidates []extract.Candidate
	calls      int
}

func (e *stubExtractor) Extract(_ context.Context, _ string, _ []memories.CurrentFact) ([]extract.Candidate, error) {
	e.calls++
	return e.candidates, nil
}

// TestSweepFindsAThreadThatStoppedShortOfTheCoachTrigger is NOR-15's gap. The
// coach enqueues extraction only once a conversation reaches four messages, so
// a thread that says something durable and stops at two was never mined at all.
func TestSweepFindsAThreadThatStoppedShortOfTheCoachTrigger(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "quiet@north.test")

	convos := conversations.NewService(conversations.NewRepository(pool))
	thread, err := convos.Start(ctx, user.ID)
	if err != nil {
		t.Fatalf("start conversation: %v", err)
	}
	if _, err = convos.AppendUserMessage(ctx, thread.ID, "I train best at 6am", nil); err != nil {
		t.Fatalf("append user message: %v", err)
	}
	if _, err = convos.AppendModelMessage(ctx, thread.ID, "Noted.", nil, "m", "p", nil); err != nil {
		t.Fatalf("append model message: %v", err)
	}

	queue := &recordingQueue{}
	// The thread was written a moment ago, so the sweep has to be told that
	// "now" is later than the idle window rather than the test waiting hours.
	sweeper := memories.NewExtractionSweeper(convos, queue, nil).
		WithClock(func() time.Time { return time.Now().Add(24 * time.Hour) })

	if err = sweeper.HandleSweep(ctx, nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	if len(queue.payloads) != 1 {
		t.Fatalf("enqueued %d jobs, want 1", len(queue.payloads))
	}
	if queue.payloads[0].ConversationID != thread.ID {
		t.Fatalf("enqueued %s, want %s", queue.payloads[0].ConversationID, thread.ID)
	}
}

// A conversation that yields nothing must still be recorded as read. Otherwise
// every sweep, forever, pays for an AI call over the same uneventful thread.
func TestAnEmptyExtractionStillMarksTheThreadRead(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "uneventful@north.test")

	convos := conversations.NewService(conversations.NewRepository(pool))
	thread, err := convos.Start(ctx, user.ID)
	if err != nil {
		t.Fatalf("start conversation: %v", err)
	}
	if _, err = convos.AppendUserMessage(ctx, thread.ID, "hello", nil); err != nil {
		t.Fatalf("append user message: %v", err)
	}
	if _, err = convos.AppendModelMessage(ctx, thread.ID, "hi", nil, "m", "p", nil); err != nil {
		t.Fatalf("append model message: %v", err)
	}

	extractor := &stubExtractor{}
	svc := &memories.ExtractionService{
		Memories:      memories.NewService(memories.NewRepository(pool)),
		Conversations: convos,
		Extractor:     extractor,
	}

	payload := mustJSON(t, jobs.ExtractMemoriesPayload{UserID: user.ID, ConversationID: thread.ID})
	if err = svc.HandleExtractJob(ctx, payload); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if extractor.calls != 1 {
		t.Fatalf("extractor ran %d times, want 1", extractor.calls)
	}

	queue := &recordingQueue{}
	sweeper := memories.NewExtractionSweeper(convos, queue, nil).
		WithClock(func() time.Time { return time.Now().Add(24 * time.Hour) })

	if err = sweeper.HandleSweep(ctx, nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(queue.payloads) != 0 {
		t.Fatalf("re-enqueued a thread already read: %+v", queue.payloads)
	}
}

// A job already waiting for this person must not be doubled by the next sweep.
func TestSweepSkipsAPersonWithExtractionAlreadyQueued(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "busy@north.test")

	convos := conversations.NewService(conversations.NewRepository(pool))
	thread, err := convos.Start(ctx, user.ID)
	if err != nil {
		t.Fatalf("start conversation: %v", err)
	}
	if _, err = convos.AppendUserMessage(ctx, thread.ID, "one", nil); err != nil {
		t.Fatalf("append user message: %v", err)
	}
	if _, err = convos.AppendModelMessage(ctx, thread.ID, "two", nil, "m", "p", nil); err != nil {
		t.Fatalf("append model message: %v", err)
	}

	queue := &recordingQueue{pending: true}
	sweeper := memories.NewExtractionSweeper(convos, queue, nil).
		WithClock(func() time.Time { return time.Now().Add(24 * time.Hour) })

	if err = sweeper.HandleSweep(ctx, nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(queue.payloads) != 0 {
		t.Fatalf("enqueued despite a pending job: %+v", queue.payloads)
	}
}
