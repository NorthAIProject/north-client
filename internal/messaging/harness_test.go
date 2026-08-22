package messaging_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/messaging"
	"github.com/NorthAIProject/north-client/internal/quota"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

// harness is one Khepri, one account, one fake model.
//
// Built on a real database for the reason testdb gives: the identity this
// package is about is a unique index and a single-use UPDATE, and a fake
// repository would prove that the Go compiles rather than that two messages
// racing on one code produce one winner.
type harness struct {
	messaging *messaging.Service
	coach     *coach.Service
	convos    *conversations.Service
	client    *fake.Client
	tools     *stubTools
	quotas    *stubQuotas
	user      users.User
	pool      *pgxpool.Pool
}

type harnessOptions struct {
	tools  *stubTools
	quotas *stubQuotas
}

func newHarness(t *testing.T, client *fake.Client, opts harnessOptions) harness {
	t.Helper()

	pool := testdb.New(t)
	ctx := context.Background()

	userSvc := users.NewService(users.NewRepository(pool))
	user, err := userSvc.Register(ctx, users.Registration{
		Email:        "fernando@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Fernando Correia",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	// Register leaves an account that still needs onboarding, and the adapter
	// refuses those on purpose. Every test here is about somebody already using
	// Khepri, so mark it done rather than repeating the questionnaire.
	user, err = userSvc.MarkOnboarded(ctx, user.ID)
	if err != nil {
		t.Fatalf("mark onboarded: %v", err)
	}

	registry := ai.NewRegistry()
	registry.Register(client)

	convos := conversations.NewService(conversations.NewRepository(pool))

	coachOpts := coach.Options{
		Registry:       registry,
		Conversations:  convos,
		ContextBuilder: coach.NewContextBuilder(convos),
		PromptBuilder:  coach.NewPromptBuilder(),
		Chains:         ai.NewChainSet([]string{client.Name()}, nil),
		Model:          "test-model",
		FastModel:      "test-fast-model",
	}
	if opts.tools != nil {
		coachOpts.Tools = opts.tools
	}
	coachSvc := coach.NewService(coachOpts)

	msgOpts := messaging.Options{
		Coach:   coachSvc,
		Threads: convos,
		Users:   userSvc,
		Links:   messaging.NewRepository(pool),
	}
	if opts.quotas != nil {
		msgOpts.Quotas = opts.quotas
	}

	return harness{
		messaging: messaging.NewService(msgOpts),
		coach:     coachSvc,
		convos:    convos,
		client:    client,
		tools:     opts.tools,
		quotas:    opts.quotas,
		user:      user,
		pool:      pool,
	}
}

// link binds a chat to the harness's account the way a person would: issue a
// code in the web app, send it to the bot.
func (h harness) link(t *testing.T, chat string) {
	t.Helper()

	code, err := h.messaging.IssueCode(context.Background(), h.user.ID, messaging.PlatformTelegram)
	if err != nil {
		t.Fatalf("issue code: %v", err)
	}
	out := h.send(t, chat, code)
	if out.Text == "" {
		t.Fatal("redeeming a code said nothing")
	}
}

// updateIDs are handed out in order so each send looks like a fresh delivery.
var updateSeq struct {
	sync.Mutex
	n int64
}

func nextUpdateID() int64 {
	updateSeq.Lock()
	defer updateSeq.Unlock()
	updateSeq.n++
	return updateSeq.n
}

func (h harness) send(t *testing.T, chat, text string) messaging.OutboundMessage {
	t.Helper()
	return h.sendUpdate(t, chat, text, nextUpdateID())
}

func (h harness) sendUpdate(t *testing.T, chat, text string, updateID int64) messaging.OutboundMessage {
	t.Helper()

	out, err := h.messaging.Handle(context.Background(), messaging.InboundMessage{
		Platform:   messaging.PlatformTelegram,
		ExternalID: chat,
		Text:       text,
		UpdateID:   updateID,
		ReceivedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("handle %q: %v", text, err)
	}
	return out
}

// stubTools stands in for internal/agent, so these tests exercise the adapter's
// confirmation path rather than any particular capability.
type stubTools struct {
	tools    []ai.Tool
	results  map[string]string
	readOnly map[string]bool

	calls [][]ai.ToolCall
}

func (s *stubTools) IsReadOnly(name string) bool { return s.readOnly[name] }
func (s *stubTools) Tools() []ai.Tool            { return s.tools }

func (s *stubTools) InvokeAll(_ context.Context, _ uuid.UUID, calls []ai.ToolCall) []ai.ToolResult {
	s.calls = append(s.calls, calls)
	out := make([]ai.ToolResult, 0, len(calls))
	for _, call := range calls {
		out = append(out, ai.ToolResult{ID: call.ID, Name: call.Name, Content: s.results[call.Name]})
	}
	return out
}

// stubQuotas puts a budget wherever a test needs it, without a clock.
type stubQuotas struct {
	allowed    bool
	retryAfter time.Duration
	consumed   int
	// tiers records the tier each call metered against, so a test can prove the
	// messaging path passes the account's real plan rather than defaulting it.
	tiers []string
}

func (s *stubQuotas) Consume(_ context.Context, _ uuid.UUID, tier string, _ quota.Action) (quota.Decision, error) {
	s.consumed++
	s.tiers = append(s.tiers, tier)
	return quota.Decision{Allowed: s.allowed, RetryAfter: s.retryAfter}, nil
}
