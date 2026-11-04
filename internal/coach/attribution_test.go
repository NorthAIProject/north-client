package coach_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/spend"
	"github.com/NorthAIProject/north-client/internal/users"
)

// recordingMeter captures what the metering decorator was told, so a test can
// assert attribution without a ledger.
type recordingMeter struct {
	mu  sync.Mutex
	got []spend.Generation
}

func (m *recordingMeter) Record(ctx context.Context, provider, model string, usage ai.Usage, byok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Reuse the real adapter so the context-reading logic under test is the
	// same code that runs in production.
	spend.NewMeter(&collect{m}).Record(ctx, provider, model, usage, byok)
}

type collect struct{ m *recordingMeter }

func (c *collect) Record(_ context.Context, g spend.Generation) { c.m.got = append(c.m.got, g) }

func (m *recordingMeter) records() []spend.Generation {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]spend.Generation(nil), m.got...)
}

// recordsOn waits for n records on a surface. Metering is the meter's business,
// so this does not assume a record lands before the stream closes.
func (m *recordingMeter) recordsOn(t *testing.T, surface string, n int) []spend.Generation {
	t.Helper()

	var got []spend.Generation
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		got = got[:0]
		for _, g := range m.records() {
			if g.Surface == surface {
				got = append(got, g)
			}
		}
		if len(got) >= n {
			break
		}
	}
	if len(got) != n {
		t.Fatalf("%d records on %q, want %d; everything recorded: %+v", len(got), surface, n, m.records())
	}
	return got
}

// attributedHarness is the tool harness with the metering decorator on.
func attributedHarness(t *testing.T, meter *recordingMeter, client *fake.Client, tools coach.ToolRunner) harness {
	t.Helper()

	pool := testdb.New(t)
	user, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        "rounds@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Rounds",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	registry := ai.NewRegistry().WithMeter(meter)
	registry.Register(client)

	convos := conversations.NewService(conversations.NewRepository(pool))
	svc := coach.NewService(coach.Options{
		Registry:       registry,
		Conversations:  convos,
		ContextBuilder: coach.NewContextBuilder(convos),
		PromptBuilder:  coach.NewPromptBuilder(),
		Tools:          tools,
		Chains:         ai.NewChainSet([]string{client.Name()}, nil),
		Model:          "test-model",
		FastModel:      "test-fast-model",
	})
	return harness{coach: svc, convos: convos, client: client, user: user, pool: pool}
}

// The rounds after a tool call carry the largest prompts of a turn. They used to
// reach the provider on a context only startChat had labelled, so the most
// expensive calls landed with no user and a surface of "unknown".
func TestEveryRoundOfAToolTurnIsAttributed(t *testing.T) {
	meter := &recordingMeter{}
	used := ai.Usage{InputTokens: 800, OutputTokens: 120}
	client := fake.New(
		fake.Response{ToolCalls: []ai.ToolCall{fake.ToolCall("search_exercises", `{"muscle":"hamstrings"}`)}, Usage: used},
		fake.Response{Text: "Try the deadlift.", Usage: used},
	)
	tools := &stubTools{
		tools:    []ai.Tool{searchTool},
		results:  map[string]string{"search_exercises": "- barbell-deadlift"},
		readOnly: map[string]bool{"search_exercises": true},
	}
	h := attributedHarness(t, meter, client, tools)
	conversationID := newConversation(t, h)

	stream, err := h.coach.SendMessage(context.Background(), h.user, conversationID, "hamstrings?")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, err := drain(stream); err != nil {
		t.Fatalf("drain: %v", err)
	}

	for _, g := range meter.recordsOn(t, spend.SurfaceCoach, 2) {
		if g.UserID == nil || *g.UserID != h.user.ID {
			t.Errorf("UserID = %v, want %v", g.UserID, h.user.ID)
		}
	}
}

// A resumed turn is the same conversation, and pays on the same line.
func TestEveryRoundOfAResumedTurnIsAttributed(t *testing.T) {
	meter := &recordingMeter{}
	used := ai.Usage{InputTokens: 800, OutputTokens: 120}
	client := fake.New(
		fake.Response{ToolCalls: []ai.ToolCall{fake.ToolCall("create_check_in", `{"mood":4}`)}, Usage: used},
		fake.Response{ToolCalls: []ai.ToolCall{fake.ToolCall("search_exercises", `{"muscle":"hamstrings"}`)}, Usage: used},
		fake.Response{Text: "Logged it.", Usage: used},
	)
	tools := &stubTools{
		tools:    []ai.Tool{writeTool, searchTool},
		results:  map[string]string{"create_check_in": "logged", "search_exercises": "- barbell-deadlift"},
		readOnly: map[string]bool{"create_check_in": false, "search_exercises": true},
	}
	h := attributedHarness(t, meter, client, tools)
	conversationID := newConversation(t, h)

	stream, err := h.coach.SendMessage(context.Background(), h.user, conversationID, "log my check-in")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, err = drain(stream); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if err = h.coach.ResolvePending(context.Background(), h.user, conversationID, uuid.Nil, true); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	stream, err = h.coach.Resume(context.Background(), h.user, conversationID)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if _, err = drain(stream); err != nil {
		t.Fatalf("drain: %v", err)
	}

	for _, g := range meter.recordsOn(t, spend.SurfaceCoach, 3) {
		if g.UserID == nil || *g.UserID != h.user.ID {
			t.Errorf("UserID = %v, want %v", g.UserID, h.user.ID)
		}
	}
}

// A coach turn must reach the provider on a context carrying the account and
// the surface.
//
// This is the test that was missing when metering went in. The meter tests call
// a wrapped client directly with an attributed context, which proves the
// decorator works and proves nothing about whether the coach ever supplies one.
// It did not: eachProvider wrapped the context it passed to ai.Runner, but
// Runner passes no context to its attempt closure, so the client was always
// handed whichever context the caller had captured — an unattributed one. Every
// coach row would have landed with no user and a surface of "unknown".
func TestACoachTurnReachesTheProviderAttributed(t *testing.T) {
	pool := testdb.New(t)

	userSvc := users.NewService(users.NewRepository(pool))
	user, err := userSvc.Register(context.Background(), users.Registration{
		Email:        "attributed@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Attributed",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	meter := &recordingMeter{}
	// Usage on every response, because the meter deliberately does not record a
	// zero-token row — a provider that reports nothing is a gap in what it
	// tells us, not a free call, and an empty row would bury the real ones. A
	// fake with no usage would make this test pass for the wrong reason.
	client := fake.New(
		fake.Response{Text: "the reply", Usage: ai.Usage{InputTokens: 800, OutputTokens: 120}},
		fake.Response{Text: "A Title", Usage: ai.Usage{InputTokens: 60, OutputTokens: 8}},
	)

	registry := ai.NewRegistry().WithMeter(meter)
	registry.Register(client)

	convos := conversations.NewService(conversations.NewRepository(pool))
	svc := coach.NewService(coach.Options{
		Registry:       registry,
		Conversations:  convos,
		ContextBuilder: coach.NewContextBuilder(convos),
		PromptBuilder:  coach.NewPromptBuilder(),
		Chains:         ai.NewChainSet([]string{client.Name()}, nil),
		Model:          "test-model",
		FastModel:      "test-fast-model",
	})

	conversation, err := convos.Start(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}

	stream, err := svc.SendMessage(context.Background(), user, conversation.ID, "hello")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, err := drain(stream); err != nil {
		t.Fatalf("drain: %v", err)
	}

	// The thread is named after the reply has been delivered, so wait for it.
	meter.recordsOn(t, spend.SurfaceTitle, 1)

	got := meter.records()
	if len(got) == 0 {
		t.Fatal("the provider was reached but nothing was metered")
	}

	// A first message produces two calls, not one: the reply, and the
	// background call that names the thread. Both must be attributed, and they
	// must not be attributed to the same surface — a title runs on the fast
	// model and is a different line item from the conversation itself.
	surfaces := map[string]int{}
	for _, g := range got {
		if g.UserID == nil || *g.UserID != user.ID {
			t.Errorf("UserID = %v, want %v; a coach call reached the provider unattributed",
				g.UserID, user.ID)
		}
		if g.Surface == spend.SurfaceUnknown || g.Surface == "" {
			t.Errorf("Surface = %q; the call reached the provider with no label", g.Surface)
		}
		surfaces[g.Surface]++
	}

	if surfaces[spend.SurfaceCoach] == 0 {
		t.Errorf("no call was recorded against %q; surfaces seen: %v", spend.SurfaceCoach, surfaces)
	}
	if surfaces[spend.SurfaceTitle] == 0 {
		t.Errorf("thread naming was not recorded against %q, so it is billed as conversation; surfaces seen: %v",
			spend.SurfaceTitle, surfaces)
	}
}
