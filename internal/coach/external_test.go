package coach_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

// stubLookups stands in for the tool audit: the exercises an agent read over
// MCP, and the moment the coach asked about.
type stubLookups struct {
	slugs []string

	mu    sync.Mutex
	since time.Time
}

func (s *stubLookups) ExercisesLookedUpSince(_ context.Context, _ uuid.UUID, since time.Time) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.since = since
	return s.slugs, nil
}

// gateway is a fake that, like a Hermes gateway, never calls the tools it is
// given: whatever it looks up, it looks up over MCP.
type gateway struct{ *fake.Client }

func (gateway) CallsTools() bool { return false }

func newExternalHarness(t *testing.T, client *fake.Client, lookups coach.ExternalLookups) harness {
	return newExternalHarnessOver(t, client, client, lookups)
}

func newExternalHarnessOver(t *testing.T, answering ai.Client, client *fake.Client, lookups coach.ExternalLookups) harness {
	t.Helper()

	pool := testdb.New(t)

	user, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        "fernando@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Fernando Correia",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	convos := conversations.NewService(conversations.NewRepository(pool))

	svc := coach.NewService(coach.Options{
		Registry:        registryOf(answering),
		Conversations:   convos,
		ContextBuilder:  coach.NewContextBuilder(convos),
		PromptBuilder:   coach.NewPromptBuilder(),
		ExternalLookups: lookups,
		Chains:          ai.NewChainSet([]string{client.Name()}, nil),
		Model:           "test-model",
		FastModel:       "test-fast-model",
	})
	return harness{coach: svc, convos: convos, client: client, user: user, pool: pool}
}

// Added after Hermes was put back first on every turn. It answers from its own
// agent, which reads the exercise over Khepri's MCP server rather than through
// the coach's tools, so the coach never sees the call. The reply still has to
// carry the exercise, or Telegram sends no animation and iOS draws no card.
func TestAnExerciseReadOverMCPDuringTheTurnIsShownWithTheReply(t *testing.T) {
	t.Parallel()

	lookups := &stubLookups{slugs: []string{"squat"}}
	answer := fake.Text("Feet shoulder-width, sit back and down.")
	h := newExternalHarnessOver(t, gateway{answer}, answer, lookups)
	conversationID := newConversation(t, h)

	before := time.Now()
	stream, err := h.coach.SendMessage(context.Background(), h.user, conversationID, "show me how to do a squat")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, drainErr := drain(stream); drainErr != nil {
		t.Fatalf("drain: %v", drainErr)
	}

	slugs, err := h.coach.LatestExerciseRefs(context.Background(), h.user, conversationID)
	if err != nil {
		t.Fatalf("latest exercise refs: %v", err)
	}
	if len(slugs) != 1 || slugs[0] != "squat" {
		t.Errorf("latest exercises = %v, want [squat]", slugs)
	}

	// Bounded by the turn, so a lookup from an earlier conversation is not
	// taken for this one.
	lookups.mu.Lock()
	since := lookups.since
	lookups.mu.Unlock()
	if since.Before(before) {
		t.Errorf("asked for lookups since %v, before the turn began at %v", since, before)
	}
}

// When the coach looked the exercise up itself, that is the answer; an agent's
// MCP traffic in the same window is not consulted.
func TestTheCoachsOwnLookupWinsOverMCPTraffic(t *testing.T) {
	t.Parallel()

	lookups := &stubLookups{slugs: []string{"push-up"}}
	client := &fake.Client{Responses: []fake.Response{
		fake.Calling(fake.ToolCall(coach.ToolGetExercise, `{"slug":"squat"}`)),
		{Text: "Feet shoulder-width, sit back and down."},
	}}
	h := newExternalHarness(t, client, lookups)
	h.coach = coach.NewService(coach.Options{
		Registry:        registryOf(client),
		Conversations:   h.convos,
		ContextBuilder:  coach.NewContextBuilder(h.convos),
		PromptBuilder:   coach.NewPromptBuilder(),
		ExternalLookups: lookups,
		Tools: &stubTools{
			tools:    []ai.Tool{{Name: coach.ToolGetExercise, Description: "Read one exercise."}},
			results:  map[string]string{coach.ToolGetExercise: "Squat (strength, beginner)"},
			readOnly: map[string]bool{coach.ToolGetExercise: true},
		},
		Chains:    ai.NewChainSet([]string{client.Name()}, nil),
		Model:     "test-model",
		FastModel: "test-fast-model",
	})
	conversationID := newConversation(t, h)

	stream, err := h.coach.SendMessage(context.Background(), h.user, conversationID, "show me how to do a squat")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, drainErr := drain(stream); drainErr != nil {
		t.Fatalf("drain: %v", drainErr)
	}

	slugs, err := h.coach.LatestExerciseRefs(context.Background(), h.user, conversationID)
	if err != nil {
		t.Fatalf("latest exercise refs: %v", err)
	}
	if len(slugs) != 1 || slugs[0] != "squat" {
		t.Errorf("latest exercises = %v, want [squat]", slugs)
	}
}

// stubLinks stands in for the catalogue's link resolver.
type stubLinks map[string]string

func (l stubLinks) SlugsLinkedIn(_ context.Context, text string) []string {
	var out []string
	for link, slug := range l {
		if strings.Contains(text, link) {
			out = append(out, slug)
		}
	}
	return out
}

// Added after Hermes answered "show me how to do a squat" with the catalogue's
// own text and links but made no MCP call the audit could see — it had them
// from somewhere else. The artwork address in the reply is still Khepri's, and
// it names the exercise exactly.
func TestAnExerciseLinkedInAGatewaysReplyIsShownWithIt(t *testing.T) {
	t.Parallel()

	answer := fake.Text("Barbell Full Squat\nSVG illustration: https://kheprios.com/assets/exercises/squat/frame-1.svg")
	h := newExternalHarnessOver(t, gateway{answer}, answer, &stubLookups{})
	h.coach = coach.NewService(coach.Options{
		Registry:        registryOf(gateway{answer}),
		Conversations:   h.convos,
		ContextBuilder:  coach.NewContextBuilder(h.convos),
		PromptBuilder:   coach.NewPromptBuilder(),
		ExternalLookups: &stubLookups{},
		ExerciseLinks:   stubLinks{"/assets/exercises/squat/": "barbell-full-squat"},
		Chains:          ai.NewChainSet([]string{answer.Name()}, nil),
		Model:           "test-model",
		FastModel:       "test-fast-model",
	})
	conversationID := newConversation(t, h)

	stream, err := h.coach.SendMessage(context.Background(), h.user, conversationID, "show me how to do a squat")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, drainErr := drain(stream); drainErr != nil {
		t.Fatalf("drain: %v", drainErr)
	}

	slugs, err := h.coach.LatestExerciseRefs(context.Background(), h.user, conversationID)
	if err != nil {
		t.Fatalf("latest exercise refs: %v", err)
	}
	if len(slugs) != 1 || slugs[0] != "barbell-full-squat" {
		t.Errorf("latest exercises = %v, want [barbell-full-squat]", slugs)
	}
}

// A provider that calls tools and looked nothing up means the reply is about
// no exercise. Another MCP client reading the catalogue at the same moment is
// not this conversation, and its lookup must not decorate this reply.
func TestAProviderThatCallsToolsIsNotCreditedWithMCPLookups(t *testing.T) {
	t.Parallel()

	lookups := &stubLookups{slugs: []string{"squat"}}
	h := newExternalHarness(t, fake.Text("Rest today; you trained hard yesterday."), lookups)
	conversationID := newConversation(t, h)

	stream, err := h.coach.SendMessage(context.Background(), h.user, conversationID, "should I train today?")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, drainErr := drain(stream); drainErr != nil {
		t.Fatalf("drain: %v", drainErr)
	}

	slugs, err := h.coach.LatestExerciseRefs(context.Background(), h.user, conversationID)
	if err != nil {
		t.Fatalf("latest exercise refs: %v", err)
	}
	if len(slugs) != 0 {
		t.Errorf("latest exercises = %v, want none", slugs)
	}
}

func registryOf(client ai.Client) *ai.Registry {
	r := ai.NewRegistry()
	r.Register(client)
	return r
}
