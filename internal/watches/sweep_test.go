package watches_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/nudges/nudge"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/watches"
)

// stubRunner stands in for the coach: it records each run and answers with a
// message in the thread it was given.
type stubRunner struct {
	mu     sync.Mutex
	runs   []string
	silent bool
}

func (r *stubRunner) RunStandingTask(_ context.Context, _ users.User, conversationID uuid.UUID, instruction string) (conversations.Message, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs = append(r.runs, instruction)
	if r.silent {
		return conversations.Message{}, false, nil
	}
	return conversations.Message{
		ID:             uuid.New(),
		ConversationID: conversationID,
		Content:        "You slept 6h10.",
		Origin:         conversations.OriginProactive,
		SourceLabel:    conversations.SourceStandingTask,
	}, true, nil
}

func (r *stubRunner) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.runs)
}

type raised struct{ kind, dedupe, title, href string }

type stubNotifier struct {
	mu   sync.Mutex
	sent []raised
}

func (n *stubNotifier) RaiseProactive(_ context.Context, _ users.User, kind, dedupe, title, _, href string) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.sent = append(n.sent, raised{kind, dedupe, title, href})
	return nil
}

type sweepHarness struct {
	svc      *watches.Service
	user     users.User
	thread   uuid.UUID
	runner   *stubRunner
	notifier *stubNotifier
	clock    *time.Time
}

func newSweepHarness(t *testing.T) sweepHarness {
	t.Helper()
	pool := testdb.New(t)
	ctx := context.Background()

	userSvc := users.NewService(users.NewRepository(pool))
	user, err := userSvc.Register(ctx, users.Registration{
		Email:        "fernando@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Fernando",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatal(err)
	}
	thread, err := conversations.NewService(conversations.NewRepository(pool)).Start(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Tuesday 22 Sept 2026, 07:00 in Lisbon.
	now := time.Date(2026, 9, 22, 6, 0, 0, 0, time.UTC)
	h := sweepHarness{user: user, thread: thread.ID, runner: &stubRunner{}, notifier: &stubNotifier{}, clock: &now}
	h.svc = watches.NewService(watches.NewRepository(pool), userSvc).WithClock(func() time.Time { return *h.clock })
	return h
}

func (h sweepHarness) sweeper() *watches.Sweeper {
	return watches.NewSweeper(h.svc, h.runner, h.notifier, nil)
}

func (h sweepHarness) createDailyAt8(t *testing.T) watches.Watch {
	t.Helper()
	w, err := h.svc.Create(context.Background(), h.user.ID, h.thread, watches.Proposal{
		Title:     "your sleep",
		Condition: "it drops under seven hours",
		Spec:      "Check last night's sleep.",
		Schedule:  watches.Schedule{Cadence: watches.CadenceDaily, Minute: 8 * 60},
	})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// Due at 8:00: not run at 7:00, run once at 8:15, and not again at 8:30.
func TestSweepRunsADueWatchOnceAndNotifies(t *testing.T) {
	t.Parallel()
	h := newSweepHarness(t)
	ctx := context.Background()
	h.createDailyAt8(t)

	if err := h.sweeper().HandleSweep(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if h.runner.count() != 0 {
		t.Fatal("a watch ran before its slot")
	}

	*h.clock = time.Date(2026, 9, 22, 7, 15, 0, 0, time.UTC) // 8:15 Lisbon
	if err := h.sweeper().HandleSweep(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if h.runner.count() != 1 {
		t.Fatalf("runs = %d, want 1", h.runner.count())
	}
	if len(h.notifier.sent) != 1 {
		t.Fatalf("notifications = %d, want 1", len(h.notifier.sent))
	}
	n := h.notifier.sent[0]
	if n.kind != nudge.KindCoachReply {
		t.Errorf("kind = %q, want coach_reply", n.kind)
	}
	if n.href != "/app/chat/"+h.thread.String() {
		t.Errorf("href = %q, want the thread", n.href)
	}

	*h.clock = time.Date(2026, 9, 22, 7, 30, 0, 0, time.UTC)
	if err := h.sweeper().HandleSweep(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if h.runner.count() != 1 {
		t.Errorf("runs = %d after a second sweep the same morning, want 1", h.runner.count())
	}

	list, _ := h.svc.List(ctx, h.user.ID)
	next := list[0].NextRunAt.In(h.user.Location())
	if next.Day() != 23 || next.Hour() != 8 {
		t.Errorf("next run = %v, want tomorrow 8:00", next)
	}
}

// Two sweeps racing on the same due row — two workers, or a duplicated
// periodic enqueue — run it exactly once. The conditional claim is the guard.
func TestConcurrentSweepsDoNotDoubleRun(t *testing.T) {
	t.Parallel()
	h := newSweepHarness(t)
	ctx := context.Background()
	h.createDailyAt8(t)
	*h.clock = time.Date(2026, 9, 22, 7, 15, 0, 0, time.UTC)

	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = h.sweeper().HandleSweep(ctx, nil)
		}()
	}
	wg.Wait()

	if h.runner.count() != 1 {
		t.Errorf("runs = %d across concurrent sweeps, want 1", h.runner.count())
	}
}

func TestASilentRunNotifiesNobody(t *testing.T) {
	t.Parallel()
	h := newSweepHarness(t)
	h.runner.silent = true
	h.createDailyAt8(t)
	*h.clock = time.Date(2026, 9, 22, 7, 15, 0, 0, time.UTC)

	if err := h.sweeper().HandleSweep(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if h.runner.count() != 1 {
		t.Fatalf("runs = %d, want 1", h.runner.count())
	}
	if len(h.notifier.sent) != 0 {
		t.Errorf("a run with nothing to say raised %d notifications", len(h.notifier.sent))
	}
}
