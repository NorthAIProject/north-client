package coach_test

import (
	"context"
	"sync"
	"testing"

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
