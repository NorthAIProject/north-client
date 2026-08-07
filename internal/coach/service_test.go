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
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
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
		// Every tier resolves to the one fake provider, so these tests exercise
		// the chain machinery without caring which tier the user is on.
		Chains:    ai.NewChainSet([]string{client.Name()}, nil),
		Model:     "test-model",
		FastModel: "test-fast-model",
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

// renamed lets one fake.Client stand in for a specific provider, since
// fake.Client is always called "fake" and a chain is a list of distinct names.
type renamed struct {
	*fake.Client
	name string
}

func (r renamed) Name() string { return r.name }

// chainHarness builds a coach whose chain is the given clients, in order.
func chainHarness(t *testing.T, clients ...ai.Client) harness {
	t.Helper()

	h := newHarness(t, fake.Text("unused"))

	registry := ai.NewRegistry()
	names := make([]string, 0, len(clients))
	for _, c := range clients {
		registry.Register(c)
		names = append(names, c.Name())
	}

	convos := conversations.NewService(conversations.NewRepository(h.pool))
	h.coach = coach.NewService(coach.Options{
		Registry:       registry,
		Conversations:  convos,
		ContextBuilder: coach.NewContextBuilder(convos),
		PromptBuilder:  coach.NewPromptBuilder(),
		Chains:         ai.NewChainSet(names, nil),
	})
	h.convos = convos

	return h
}

// refusing is a client that fails before streaming starts, the way a provider
// out of credit does.
func refusing(name string, err error) ai.Client {
	return renamed{
		Client: &fake.Client{Handler: func(context.Context, ai.Request) (fake.Response, error) {
			return fake.Response{}, err
		}},
		name: name,
	}
}

// The point of the whole exercise: an exhausted OpenRouter balance must not be
// something the user ever sees.
func TestChainFallsOverToTheNextProvider(t *testing.T) {
	broke := refusing("broke", apperr.Wrap(apperr.ErrPaymentRequired, "no credit"))
	backup := renamed{Client: fake.Text("Deadlift day."), name: "backup"}

	h := chainHarness(t, broke, backup)
	ctx := context.Background()

	conversation, _ := h.coach.StartConversation(ctx, h.user.ID)

	stream, err := h.coach.SendMessage(ctx, h.user, conversation.ID, "what next?")
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	reply, err := drain(stream)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if !strings.Contains(reply, "Deadlift day.") {
		t.Fatalf("reply = %q, want the backup provider's answer", reply)
	}

	// The provider that actually answered is what gets attributed, or the
	// stored history would credit a provider that refused.
	history, err := h.convos.History(ctx, conversation.ID)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected both turns, got %d", len(history))
	}
	if got := history[1].Provider; got != "backup" {
		t.Errorf("provider = %q, want backup", got)
	}
}

// A malformed request fails the same way everywhere, so walking the chain only
// delays the error the user was always going to get.
func TestChainDoesNotFailOverOnCallerErrors(t *testing.T) {
	first := refusing("first", apperr.Wrap(apperr.ErrValidation, "bad request"))
	second := renamed{Client: fake.Text("should never be reached"), name: "second"}

	h := chainHarness(t, first, second)
	ctx := context.Background()

	conversation, _ := h.coach.StartConversation(ctx, h.user.ID)

	if _, err := h.coach.SendMessage(ctx, h.user, conversation.ID, "what next?"); err == nil {
		t.Fatal("expected the validation error to surface")
	}

	if calls := second.Client.Calls(); len(calls) != 0 {
		t.Fatalf("the second provider should not have been asked, got %d calls", len(calls))
	}
}

// When every provider refuses there is nothing left to try, and the last
// failure is what the user's error should be built from.
func TestChainSurfacesTheLastErrorWhenAllRefuse(t *testing.T) {
	h := chainHarness(t,
		refusing("one", apperr.Wrap(apperr.ErrUnavailable, "overloaded")),
		refusing("two", apperr.Wrap(apperr.ErrPaymentRequired, "no credit")),
	)
	ctx := context.Background()

	conversation, _ := h.coach.StartConversation(ctx, h.user.ID)

	_, err := h.coach.SendMessage(ctx, h.user, conversation.ID, "what next?")
	if err == nil {
		t.Fatal("expected an error once every provider refused")
	}
	if !apperr.Is(err, apperr.ErrPaymentRequired) {
		t.Fatalf("error = %v, want the last provider's failure", err)
	}
}

// tierHarness builds a coach with a different chain per tier.
func tierHarness(t *testing.T, fallback, free ai.Client) harness {
	t.Helper()

	h := newHarness(t, fake.Text("unused"))

	registry := ai.NewRegistry()
	registry.Register(fallback)
	registry.Register(free)

	convos := conversations.NewService(conversations.NewRepository(h.pool))
	h.coach = coach.NewService(coach.Options{
		Registry:       registry,
		Conversations:  convos,
		ContextBuilder: coach.NewContextBuilder(convos),
		PromptBuilder:  coach.NewPromptBuilder(),
		Chains: ai.NewChainSet(
			[]string{fallback.Name()},
			map[string][]string{string(users.TierFree): {free.Name()}},
		),
	})
	h.convos = convos

	return h
}

// A free account must not be answered by the paid provider. This is the whole
// reason the tier column exists.
func TestFreeTierUsesTheFreeChain(t *testing.T) {
	paid := renamed{Client: fake.Text("from the paid provider"), name: "paid"}
	free := renamed{Client: fake.Text("from the free provider"), name: "free"}

	h := tierHarness(t, paid, free)
	ctx := context.Background()

	if h.user.Tier != users.TierFree {
		t.Fatalf("a new account should start on the free tier, got %q", h.user.Tier)
	}

	conversation, _ := h.coach.StartConversation(ctx, h.user.ID)

	stream, err := h.coach.SendMessage(ctx, h.user, conversation.ID, "what next?")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	reply, err := drain(stream)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}

	if !strings.Contains(reply, "from the free provider") {
		t.Fatalf("reply = %q, want the free provider's answer", reply)
	}
	if len(paid.Client.Calls()) != 0 {
		t.Errorf("the paid provider was called %d time(s) for a free user", len(paid.Client.Calls()))
	}
}

// A tier with no chain of its own falls back to the main one, so introducing a
// tier cannot leave its users with no provider at all.
func TestUnknownTierFallsBackToTheMainChain(t *testing.T) {
	paid := renamed{Client: fake.Text("from the paid provider"), name: "paid"}
	free := renamed{Client: fake.Text("from the free provider"), name: "free"}

	h := tierHarness(t, paid, free)
	ctx := context.Background()

	pro := h.user
	pro.Tier = users.TierPro

	conversation, _ := h.coach.StartConversation(ctx, pro.ID)

	stream, err := h.coach.SendMessage(ctx, pro, conversation.ID, "what next?")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	reply, err := drain(stream)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}

	if !strings.Contains(reply, "from the paid provider") {
		t.Fatalf("reply = %q, want the main chain's answer", reply)
	}
}
