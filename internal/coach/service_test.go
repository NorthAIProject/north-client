package coach_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

type harness struct {
	coach  *coach.Service
	convos *conversations.Service
	client *fake.Client
	user   users.User
	pool   *pgxpool.Pool
}

func newHarness(t *testing.T, client *fake.Client) harness {
	t.Helper()

	pool := testdb.New(t)

	userSvc := users.NewService(users.NewRepository(pool))
	user, err := userSvc.Register(context.Background(), users.Registration{
		Email:        "fernando@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Fernando Correia",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	registry := ai.NewRegistry()
	registry.Register(client)

	convos := conversations.NewService(conversations.NewRepository(pool))

	svc := coach.NewService(coach.Options{
		Registry:       registry,
		Conversations:  convos,
		ContextBuilder: coach.NewContextBuilder(convos),
		PromptBuilder:  coach.NewPromptBuilder(),
		Model:          "test-model",
		FastModel:      "test-fast-model",
	})

	return harness{coach: svc, convos: convos, client: client, user: user, pool: pool}
}

func drain(ch <-chan ai.StreamChunk) (string, error) {
	var out strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			return out.String(), chunk.Err
		}
		out.WriteString(chunk.Text)
	}
	return out.String(), nil
}

// waitForReply polls for the stored model turn. Persistence happens on a
// detached context after the stream closes, so it is not synchronous with the
// caller finishing.
func waitForReply(t *testing.T, h harness, conversationID uuid.UUID, timeout time.Duration) conversations.Message {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		history, err := h.convos.History(context.Background(), conversationID)
		if err != nil {
			t.Fatalf("history: %v", err)
		}
		for _, m := range history {
			if m.IsModel() {
				return m
			}
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatal("the coach's reply was never stored")
	return conversations.Message{}
}

func TestSendMessageStreamsAndStoresBothTurns(t *testing.T) {
	h := newHarness(t, fake.Text("Add two and a half kilos next session."))
	ctx := context.Background()

	conversation, err := h.coach.StartConversation(ctx, h.user.ID)
	if err != nil {
		t.Fatalf("start conversation: %v", err)
	}

	stream, err := h.coach.SendMessage(ctx, h.user, conversation.ID, "What should I do next session?")
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	reply, err := drain(stream)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	if reply != "Add two and a half kilos next session." {
		t.Fatalf("streamed reply = %q", reply)
	}

	stored := waitForReply(t, h, conversation.ID, 3*time.Second)
	if stored.Content != reply {
		t.Fatalf("stored reply %q does not match what was streamed %q", stored.Content, reply)
	}
	if stored.Model != "test-model" || stored.Provider != "fake" {
		t.Errorf("reply is missing provenance: model=%q provider=%q", stored.Model, stored.Provider)
	}

	history, err := h.convos.History(ctx, conversation.ID)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected the user turn and the reply, got %d messages", len(history))
	}
	if !history[0].IsUser() || history[0].Content != "What should I do next session?" {
		t.Errorf("first message should be the user's: %+v", history[0])
	}
}

// The behaviour the detached-context design exists for. A user who closes the
// tab mid-answer loses the live view, not the answer.
func TestReplyIsStoredEvenWhenTheUserDisconnects(t *testing.T) {
	client := fake.Text("This is a long considered reply that keeps going for a while.")
	client.ChunkDelay = 5 * time.Millisecond

	h := newHarness(t, client)

	conversation, err := h.coach.StartConversation(context.Background(), h.user.ID)
	if err != nil {
		t.Fatalf("start conversation: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	stream, err := h.coach.SendMessage(ctx, h.user, conversation.ID, "Talk to me about my training.")
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	// Read one chunk, then vanish.
	<-stream
	cancel()

	stored := waitForReply(t, h, conversation.ID, 5*time.Second)

	if stored.Content != "This is a long considered reply that keeps going for a while." {
		t.Fatalf("the stored reply is truncated or missing: %q", stored.Content)
	}
}

func TestSendMessageRejectsEmptyInput(t *testing.T) {
	h := newHarness(t, fake.Text("unused"))
	ctx := context.Background()

	conversation, err := h.coach.StartConversation(ctx, h.user.ID)
	if err != nil {
		t.Fatalf("start conversation: %v", err)
	}

	if _, err := h.coach.SendMessage(ctx, h.user, conversation.ID, "   "); err == nil {
		t.Fatal("an empty message must be rejected")
	}

	// Nothing should have been stored, and no provider call should have been made.
	history, _ := h.convos.History(ctx, conversation.ID)
	if len(history) != 0 {
		t.Fatalf("a rejected message stored %d row(s)", len(history))
	}
	if len(h.client.Calls()) != 0 {
		t.Fatalf("a rejected message still called the provider %d time(s)", len(h.client.Calls()))
	}
}

// A conversation belongs to its owner. Reaching another user's thread by ID
// must fail even with a valid session.
func TestSendMessageRefusesAnotherUsersConversation(t *testing.T) {
	h := newHarness(t, fake.Text("unused"))
	ctx := context.Background()

	userSvc := users.NewService(users.NewRepository(h.pool))
	intruder, err := userSvc.Register(ctx, users.Registration{
		Email:        "someone-else@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Someone Else",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatalf("create second user: %v", err)
	}

	conversation, err := h.coach.StartConversation(ctx, h.user.ID)
	if err != nil {
		t.Fatalf("start conversation: %v", err)
	}

	if _, err := h.coach.SendMessage(ctx, intruder, conversation.ID, "let me in"); err == nil {
		t.Fatal("writing to another user's conversation must fail")
	}
}

// The grounding contract is only worth anything if the context actually reaches
// the model. This asserts the system prompt carries both the rules and the
// user's details.
func TestSystemPromptCarriesRulesAndContext(t *testing.T) {
	h := newHarness(t, fake.Text("ok"))
	ctx := context.Background()

	conversation, _ := h.coach.StartConversation(ctx, h.user.ID)

	stream, err := h.coach.SendMessage(ctx, h.user, conversation.ID, "hello")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, err := drain(stream); err != nil {
		t.Fatalf("stream: %v", err)
	}

	system := h.client.Calls()[0].System

	for _, want := range []string{
		"never state a fact about this person that is not in the context",
		"## CONTEXT",
		"Fernando Correia",
		"Europe/Lisbon",
		// Absent sources are labelled rather than omitted, so the model knows
		// they are empty instead of assuming.
		"Goals: none recorded yet",
	} {
		if !strings.Contains(strings.ToLower(system), strings.ToLower(want)) {
			t.Errorf("system prompt is missing %q\n---\n%s", want, system)
		}
	}
}

// History must reach the model, or the coach restarts from nothing every turn
// and the whole product premise fails.
func TestConversationHistoryIsSentToTheModel(t *testing.T) {
	h := newHarness(t, fake.Text("ok"))
	ctx := context.Background()

	conversation, _ := h.coach.StartConversation(ctx, h.user.ID)

	for _, msg := range []string{"my knee hurts", "it started last week"} {
		stream, err := h.coach.SendMessage(ctx, h.user, conversation.ID, msg)
		if err != nil {
			t.Fatalf("send %q: %v", msg, err)
		}
		if _, err := drain(stream); err != nil {
			t.Fatalf("stream: %v", err)
		}
		waitForReply(t, h, conversation.ID, 3*time.Second)
	}

	last := h.client.Calls()[len(h.client.Calls())-1]

	var transcript strings.Builder
	for _, m := range last.Messages {
		transcript.WriteString(m.Text())
		transcript.WriteString("\n")
	}

	if !strings.Contains(transcript.String(), "my knee hurts") {
		t.Errorf("the earlier turn was not sent to the model:\n%s", transcript.String())
	}
	if !strings.Contains(transcript.String(), "it started last week") {
		t.Errorf("the current turn was not sent to the model:\n%s", transcript.String())
	}
}

// A provider failure must not lose the user's message: they said something, and
// the transcript has to agree with that.
func TestProviderFailureKeepsTheUserMessage(t *testing.T) {
	h := newHarness(t, fake.New(fake.Response{Err: errors.New("provider is down")}))
	ctx := context.Background()

	conversation, _ := h.coach.StartConversation(ctx, h.user.ID)

	stream, err := h.coach.SendMessage(ctx, h.user, conversation.ID, "are you there?")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, err = drain(stream); err == nil {
		t.Fatal("the stream should have reported the provider failure")
	}

	history, err := h.convos.History(ctx, conversation.ID)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 1 || !history[0].IsUser() {
		t.Fatalf("expected only the user's message to survive, got %d", len(history))
	}
}
