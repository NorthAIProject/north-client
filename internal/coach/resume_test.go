package coach_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/conversations"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/watches"
)

// modelCalls counts the turns the model was asked to write, leaving out the
// fast model naming the thread.
func modelCalls(client *fake.Client) int {
	n := 0
	for _, req := range client.Calls() {
		if req.Model == "test-model" {
			n++
		}
	}
	return n
}

// replies are the stored turns that carry words from the coach.
func replies(t *testing.T, h harness, conversationID uuid.UUID) []conversations.Message {
	t.Helper()
	history, err := h.convos.History(context.Background(), conversationID)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	var out []conversations.Message
	for _, m := range history {
		if m.IsModel() && !m.IsToolTurn() {
			out = append(out, m)
		}
	}
	return out
}

// owedAReply seeds a thread whose approval was answered and whose reply has not
// been written, the only state /resume is for.
func owedAReply(t *testing.T, h harness) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	conversationID := newConversation(t, h)
	if _, err := h.convos.AppendUserMessage(ctx, conversationID, "log my check-in", nil); err != nil {
		t.Fatal(err)
	}
	calls := []ai.ToolCall{{ID: "c1", Name: "create_check_in", Arguments: json.RawMessage(`{"mood":4}`)}}
	if _, err := h.convos.AppendToolCalls(ctx, conversationID, calls, nil); err != nil {
		t.Fatal(err)
	}
	results := []ai.ToolResult{{ID: "c1", Name: "create_check_in", Content: "logged"}}
	if _, err := h.convos.AppendToolResults(ctx, conversationID, results); err != nil {
		t.Fatal(err)
	}
	return conversationID
}

func wantConflict(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, apperr.ErrConflict) {
		t.Fatalf("resume error = %v, want ErrConflict", err)
	}
}

// A request ending on the coach's own words is assistant prefill, which Haiku 5.5
// refuses with a 400 and the next provider "continues". The endpoint spends no
// quota, so each repeat was also a free model turn.
func TestResumeAfterACompletedReplyIsAConflict(t *testing.T) {
	t.Parallel()

	client := fake.Text("should not be generated")
	h := newHarness(t, client)
	ctx := context.Background()
	conversationID := owedAReply(t, h)
	if _, err := h.convos.AppendModelMessage(ctx, conversationID, "Logged it.", nil, "m", "p", nil, conversations.Provenance{}); err != nil {
		t.Fatal(err)
	}

	_, err := h.coach.Resume(ctx, h.user, conversationID)
	wantConflict(t, err)
	if n := len(client.Calls()); n != 0 {
		t.Errorf("the model was called %d times", n)
	}
}

// A call still waiting on a person has no result to answer from, and every
// provider refuses it.
func TestResumeWhileACallIsStillPendingIsAConflict(t *testing.T) {
	t.Parallel()

	client := fake.Text("should not be generated")
	h := newHarness(t, client)
	ctx := context.Background()
	conversationID := newConversation(t, h)
	if _, err := h.convos.AppendUserMessage(ctx, conversationID, "log my check-in", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := h.convos.AppendToolCalls(ctx, conversationID, []ai.ToolCall{{ID: "c1", Name: "create_check_in"}}, nil); err != nil {
		t.Fatal(err)
	}

	_, err := h.coach.Resume(ctx, h.user, conversationID)
	wantConflict(t, err)
	if n := len(client.Calls()); n != 0 {
		t.Errorf("the model was called %d times", n)
	}
}

func TestResumeOfAnEmptyThreadIsAConflict(t *testing.T) {
	t.Parallel()

	client := fake.Text("should not be generated")
	h := newHarness(t, client)

	_, err := h.coach.Resume(context.Background(), h.user, newConversation(t, h))
	wantConflict(t, err)
	if n := len(client.Calls()); n != 0 {
		t.Errorf("the model was called %d times", n)
	}
}

func TestTwoConcurrentResumesProduceOneReply(t *testing.T) {
	t.Parallel()

	client := fake.Text("Logged it.")
	h := newHarness(t, client)
	conversationID := owedAReply(t, h)

	var (
		wg        sync.WaitGroup
		start     = make(chan struct{})
		mu        sync.Mutex
		conflicts int
		streams   int
	)
	for range 2 {
		wg.Go(func() {
			<-start
			stream, err := h.coach.Resume(context.Background(), h.user, conversationID)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if !errors.Is(err, apperr.ErrConflict) {
					t.Errorf("resume error = %v, want ErrConflict", err)
				}
				conflicts++
				return
			}
			streams++
			if _, err := drain(stream); err != nil {
				t.Errorf("drain: %v", err)
			}
		})
	}
	close(start)
	wg.Wait()

	if streams != 1 || conflicts != 1 {
		t.Errorf("streams = %d, conflicts = %d, want one of each", streams, conflicts)
	}
	if got := replies(t, h, conversationID); len(got) != 1 {
		t.Errorf("stored replies = %d, want 1", len(got))
	}
	if n := modelCalls(client); n != 1 {
		t.Errorf("model turns = %d, want 1", n)
	}
}

// The guard has to outlast Resume returning. The reply is written after that,
// and until it is stored the thread still reads as owed one.
func TestAResumeInFlightHoldsOffASecondOne(t *testing.T) {
	t.Parallel()

	client := fake.Text("Logged it.")
	h := newHarness(t, client)
	ctx := context.Background()
	conversationID := owedAReply(t, h)

	owed, err := h.coach.AwaitingResume(ctx, conversationID)
	if err != nil || !owed {
		t.Fatalf("awaiting resume = %v, %v; want true before anything has started", owed, err)
	}

	first, err := h.coach.Resume(ctx, h.user, conversationID)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}

	// Nothing has read the stream, so the reply cannot have been stored yet.
	_, err = h.coach.Resume(ctx, h.user, conversationID)
	wantConflict(t, err)

	// A device opening the thread now must wait for the reply, not ask for it
	// and be told no: clients open the stream whenever this says true.
	if owed, err = h.coach.AwaitingResume(ctx, conversationID); err != nil || owed {
		t.Errorf("awaiting resume = %v, %v while one is running; want false", owed, err)
	}

	if _, err = drain(first); err != nil {
		t.Fatalf("drain: %v", err)
	}
	_, err = h.coach.Resume(ctx, h.user, conversationID)
	wantConflict(t, err)
	if got := replies(t, h, conversationID); len(got) != 1 {
		t.Errorf("stored replies = %d, want 1", len(got))
	}
}

// A provider failing is the one reason to ask again, so the guard must be gone
// by the time the stream reports it.
func TestAFailedResumeCanBeRetried(t *testing.T) {
	t.Parallel()

	client := fake.New(fake.Response{Err: apperr.ErrUnavailable}, fake.Response{Text: "Logged it."})
	h := newHarness(t, client)
	ctx := context.Background()
	conversationID := owedAReply(t, h)

	stream, err := h.coach.Resume(ctx, h.user, conversationID)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if _, err = drain(stream); !errors.Is(err, apperr.ErrUnavailable) {
		t.Fatalf("drain error = %v, want the provider's", err)
	}
	// drain stops at the error, so read on to the close the guard is released by.
	for range stream { //nolint:revive // Waiting for the close.
	}

	stream, err = h.coach.Resume(ctx, h.user, conversationID)
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if text, err := drain(stream); err != nil || text != "Logged it." {
		t.Fatalf("retry = %q, %v", text, err)
	}
}

// A resume that asks for another write leaves a card, and answering that card
// must not find the guard still held.
func TestAResumeThatStopsForAnotherApprovalCanBeAnswered(t *testing.T) {
	t.Parallel()

	tools := &stubTools{
		tools:    []ai.Tool{writeTool},
		results:  map[string]string{"create_check_in": "logged"},
		readOnly: map[string]bool{"create_check_in": false},
	}
	client := fake.New(
		fake.Calling(fake.ToolCall("create_check_in", `{"mood":5}`)),
		fake.Response{Text: "Logged both."},
	)
	h := newToolHarness(t, client, tools)
	ctx := context.Background()
	conversationID := owedAReply(t, h)

	stream, err := h.coach.Resume(ctx, h.user, conversationID)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if _, err = drain(stream); err != nil {
		t.Fatalf("drain: %v", err)
	}

	if err = h.coach.ResolvePending(ctx, h.user, conversationID, uuid.Nil, true); err != nil {
		t.Fatalf("resolve the second card: %v", err)
	}
	stream, err = h.coach.Resume(ctx, h.user, conversationID)
	if err != nil {
		t.Fatalf("resume again: %v", err)
	}
	if text, err := drain(stream); err != nil || text != "Logged both." {
		t.Fatalf("reply = %q, %v", text, err)
	}
}

func TestResumeOfSomebodyElsesThreadIsNotFoundEvenWhileBusy(t *testing.T) {
	t.Parallel()

	h := newHarness(t, fake.Text("Logged it."))
	ctx := context.Background()
	conversationID := owedAReply(t, h)

	stranger, err := users.NewService(users.NewRepository(h.pool)).Register(ctx, users.Registration{
		Email:        "stranger@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Stranger",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}

	first, err := h.coach.Resume(ctx, h.user, conversationID)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	defer func() { _, _ = drain(first) }()

	// A conflict here would tell a stranger the thread exists.
	if _, err = h.coach.Resume(ctx, stranger, conversationID); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("resume error = %v, want ErrNotFound", err)
	}
}

// iOS and Telegram call resume after every approval. A confirmed standing task
// is already answered by then, with a fixed sentence and no generation, so these
// callers are handed that sentence: refusing would report a failure for a watch
// that was created, and generating would add a turn nobody asked for.
func TestResumeAfterAConfirmedStandingTaskHandsBackTheSentence(t *testing.T) {
	t.Parallel()

	client := &fake.Client{Responses: []fake.Response{
		fake.Calling(fake.ToolCall(watches.ToolName, sleepWatchArgs)),
		{Text: "this reply must never be generated"},
	}}
	h := newStandingHarness(t, client)
	conversationID := proposeWatch(t, h)
	ctx := context.Background()

	if err := h.coach.ResolvePending(ctx, h.user, conversationID, uuid.Nil, true); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	before := len(client.Calls())
	stored := replies(t, h.harness, conversationID)

	// Twice, because a repeat is as free as the first.
	for range 2 {
		stream, err := h.coach.Resume(ctx, h.user, conversationID)
		if err != nil {
			t.Fatalf("resume: %v", err)
		}
		text, err := drain(stream)
		if err != nil {
			t.Fatalf("drain: %v", err)
		}
		if !strings.HasPrefix(text, "Got it") {
			t.Errorf("reply = %q, want the confirmation sentence", text)
		}
	}

	if n := len(client.Calls()) - before; n != 0 {
		t.Errorf("the model was called %d more times", n)
	}
	if got := replies(t, h.harness, conversationID); len(got) != len(stored) {
		t.Errorf("stored replies went from %d to %d", len(stored), len(got))
	}
}

// A standing task that fired on its schedule looks like a confirmation from the
// last row alone. Nobody approved anything, so there is nothing to hand back.
func TestResumeAfterAFiredStandingTaskIsAConflict(t *testing.T) {
	t.Parallel()

	client := fake.Text("should not be generated")
	h := newHarness(t, client)
	ctx := context.Background()
	conversationID := newConversation(t, h)
	if _, err := h.convos.AppendUserMessage(ctx, conversationID, "how did I sleep?", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := h.convos.AppendModelMessage(ctx, conversationID, "Under seven hours.", nil, "", "", nil,
		conversations.Proactive(conversations.SourceStandingTask)); err != nil {
		t.Fatal(err)
	}

	_, err := h.coach.Resume(ctx, h.user, conversationID)
	wantConflict(t, err)
	if n := len(client.Calls()); n != 0 {
		t.Errorf("the model was called %d times", n)
	}
}

// The default answer on /resume is what a client shows, and "this reflection has
// ended" is the only conflict wording the rest of the API has.
func TestAResumeRefusedSaysWhatHappened(t *testing.T) {
	t.Parallel()

	f := newThreadFixture(t, fake.Text("unused"))
	conversationID, _ := f.seedThread(t, 2)

	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/conversations/"+conversationID.String()+"/resume", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("resume = %d, want 409", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "reflection") {
		t.Errorf("body = %s, which blames a reflection", rec.Body.String())
	}
}
