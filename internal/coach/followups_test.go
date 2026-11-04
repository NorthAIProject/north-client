package coach_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/jobs"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/shared/lifecycle"
	"github.com/NorthAIProject/north-client/internal/users"
)

// slowTitles answers the reply straight away and holds the title call until told,
// which is a provider taking its time over the one call nobody is waiting for.
type slowTitles struct {
	client  *fake.Client
	started chan context.Context
	release chan struct{}
}

func newSlowTitles() *slowTitles {
	s := &slowTitles{started: make(chan context.Context, 4), release: make(chan struct{})}
	s.client = &fake.Client{Handler: func(ctx context.Context, req ai.Request) (fake.Response, error) {
		if req.Model != "test-fast-model" {
			return fake.Response{Text: "The reply."}, nil
		}
		s.started <- ctx
		select {
		case <-s.release:
			return fake.Response{Text: "A Title"}, nil
		case <-ctx.Done():
			return fake.Response{}, ctx.Err()
		}
	}}
	return s
}

type followUpFixture struct {
	harness
	background *lifecycle.Tracker
}

func newFollowUpFixture(t *testing.T, client *fake.Client) followUpFixture {
	t.Helper()

	pool := testdb.New(t)
	user, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        "follow-ups@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Follow Ups",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	registry := ai.NewRegistry()
	registry.Register(client)
	convos := conversations.NewService(conversations.NewRepository(pool))
	background := lifecycle.NewTracker(slog.New(slog.DiscardHandler))

	svc := coach.NewService(coach.Options{
		Background:     background,
		Registry:       registry,
		Conversations:  convos,
		ContextBuilder: coach.NewContextBuilder(convos),
		PromptBuilder:  coach.NewPromptBuilder(),
		Queue:          jobs.NewQueue(pool),
		Chains:         ai.NewChainSet([]string{client.Name()}, nil),
		Model:          "test-model",
		FastModel:      "test-fast-model",
	})
	return followUpFixture{
		harness:    harness{coach: svc, convos: convos, client: client, user: user, pool: pool},
		background: background,
	}
}

// closes waits for a stream to end, failing rather than hanging when it does not.
func closes(t *testing.T, stream <-chan ai.StreamChunk) {
	t.Helper()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range stream { //nolint:revive // Waiting for the close.
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the stream did not close while the title was still being written")
	}
}

func (f followUpFixture) jobsOfKind(t *testing.T, kind jobs.Kind) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM jobs WHERE kind = $1`, string(kind)).Scan(&n); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	return n
}

// done is the client's cue that the reply is stored. Naming the thread is a whole
// model call that nobody is waiting on, and used to hold that cue back.
func TestDoneDoesNotWaitForTheTitle(t *testing.T) {
	t.Parallel()

	titles := newSlowTitles()
	f := newFollowUpFixture(t, titles.client)
	ctx := context.Background()
	conversationID := newConversation(t, f.harness)

	stream, err := f.coach.SendMessage(ctx, f.user, conversationID, "How is my training going?")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	closes(t, stream)

	// Stored by the time the stream closes: the handler reads it for done.
	if got := replies(t, f.harness, conversationID); len(got) != 1 || got[0].Content != "The reply." {
		t.Fatalf("stored replies = %+v, want the reply", got)
	}

	var titleCtx context.Context
	select {
	case titleCtx = <-titles.started:
	case <-time.After(10 * time.Second):
		t.Fatal("the thread was never named")
	}

	// A context of its own, alive after the turn's has been cancelled and with
	// its own limit, so a stuck provider cannot hold it for ever.
	if titleErr := titleCtx.Err(); titleErr != nil {
		t.Errorf("the title's context is already finished: %v", titleErr)
	}
	if _, bounded := titleCtx.Deadline(); !bounded {
		t.Error("the title's context has no deadline")
	}

	// The call is still running, so a shutdown has something to wait for.
	if f.background.Drain(50 * time.Millisecond) {
		t.Error("a shutdown did not wait for the title being written")
	}

	close(titles.release)
	if !f.background.Drain(10 * time.Second) {
		t.Fatal("the title never finished")
	}
	conversation, err := f.convos.Get(ctx, conversationID, f.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if conversation.Title != "A Title" {
		t.Errorf("title = %q", conversation.Title)
	}
}

// They used to run one after another on a single 15 second budget, so a title
// that used all of it silently cost the turn its summary and memory extraction.
func TestASlowTitleCostsTheOtherFollowUpsNothing(t *testing.T) {
	t.Parallel()

	titles := newSlowTitles()
	f := newFollowUpFixture(t, titles.client)
	ctx := context.Background()
	conversationID := newConversation(t, f.harness)

	// Long enough that the thread needs compacting and has something to extract.
	for i := range f.coachRecentTurns() + 2 {
		var err error
		if i%2 == 0 {
			_, err = f.convos.AppendUserMessage(ctx, conversationID, "question", nil)
		} else {
			_, err = f.convos.AppendModelMessage(ctx, conversationID, "answer", nil, "m", "p", nil, conversations.Provenance{})
		}
		if err != nil {
			t.Fatal(err)
		}
	}

	stream, err := f.coach.SendMessage(ctx, f.user, conversationID, "And now?")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	closes(t, stream)

	select {
	case <-titles.started:
	case <-time.After(10 * time.Second):
		t.Fatal("the thread was never named")
	}

	if n := f.jobsOfKind(t, jobs.KindSummarizeConversation); n != 1 {
		t.Errorf("summary jobs = %d, want 1 while the title is still running", n)
	}
	if n := f.jobsOfKind(t, jobs.KindExtractMemories); n != 1 {
		t.Errorf("extraction jobs = %d, want 1 while the title is still running", n)
	}

	close(titles.release)
	if !f.background.Drain(10 * time.Second) {
		t.Fatal("the title never finished")
	}
}

// A client reads the conversation as soon as the stream closes, and a finished
// reflection has to read as finished by then or it offers a composer that refuses.
func TestAFinishedReflectionHasEndedWhenTheStreamCloses(t *testing.T) {
	t.Parallel()

	client := fake.Text("Good session.\n\n## Reflection summary\nYou slept better.")
	f := newFollowUpFixture(t, client)
	ctx := context.Background()

	conversation, err := f.coach.StartReflection(ctx, f.user.ID)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	stream, err := f.coach.SendMessage(ctx, f.user, conversation.ID, "I slept better.")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	closes(t, stream)

	got, err := f.convos.Get(ctx, conversation.ID, f.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Ended() {
		t.Error("the stream closed before the reflection read as ended")
	}
	f.background.Drain(10 * time.Second)
}

// The handler returns when the stream closes, which cancels the request. Asking
// whether the caller has gone after that would answer yes for every reply, and
// everybody who read one to the end would be told it was ready.
func TestAReplyReadToTheEndIsNotAnnounced(t *testing.T) {
	t.Parallel()

	f := newFollowUpFixture(t, fake.Text("The reply."))
	announced := make(chan string, 1)
	f.coach.WithAway(coach.InboxFunc(func(_ context.Context, _ users.User, _, dedupe, _, _, _ string) error {
		announced <- dedupe
		return nil
	}))

	ctx, cancel := context.WithCancel(context.Background())
	stream, err := f.coach.SendMessage(ctx, f.user, newConversation(t, f.harness), "How is my training going?")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	closes(t, stream)
	cancel()

	if !f.background.Drain(10 * time.Second) {
		t.Fatal("the follow-ups never finished")
	}
	select {
	case id := <-announced:
		t.Errorf("a reply read live was announced: %s", id)
	default:
	}
}

// Turning the title into a follow-up opened a window: a second message sent right
// after done still holds a conversation read before the first title landed.
func TestAThreadNamedMeanwhileIsNotNamedAgain(t *testing.T) {
	t.Parallel()

	titled := 0
	client := &fake.Client{Handler: func(_ context.Context, req ai.Request) (fake.Response, error) {
		if req.Model == "test-fast-model" {
			titled++
			return fake.Response{Text: "A Second Title"}, nil
		}
		return fake.Response{Text: "The reply."}, nil
	}}
	f := newFollowUpFixture(t, client)
	ctx := context.Background()
	conversationID := newConversation(t, f.harness)

	// Reading the stream paces the turn: the conversation has been read by now and
	// the reply has not been stored.
	stream, err := f.coach.SendMessage(ctx, f.user, conversationID, "How is my training going?")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if err = f.convos.SetTitle(ctx, conversationID, "A First Title"); err != nil {
		t.Fatal(err)
	}
	closes(t, stream)
	if !f.background.Drain(10 * time.Second) {
		t.Fatal("the follow-ups never finished")
	}

	if titled != 0 {
		t.Errorf("the thread was named %d more times", titled)
	}
	got, err := f.convos.Get(ctx, conversationID, f.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "A First Title" {
		t.Errorf("title = %q", got.Title)
	}
}

// coachRecentTurns is the tail the coach sends, past which a thread is compacted.
func (f followUpFixture) coachRecentTurns() int {
	return coach.NewContextBuilder(f.convos).RecentTurns()
}
