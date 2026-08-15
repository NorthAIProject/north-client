package coach_test

import (
	"context"
	"strings"
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

// stubTools stands in for internal/agent's registry, so these tests exercise
// the coach's loop rather than any particular capability.
type stubTools struct {
	tools   []ai.Tool
	results map[string]string

	calls  [][]ai.ToolCall
	userID uuid.UUID
}

func (s *stubTools) Tools() []ai.Tool { return s.tools }

func (s *stubTools) InvokeAll(_ context.Context, userID uuid.UUID, calls []ai.ToolCall) []ai.ToolResult {
	s.calls = append(s.calls, calls)
	s.userID = userID

	out := make([]ai.ToolResult, 0, len(calls))
	for _, call := range calls {
		out = append(out, ai.ToolResult{ID: call.ID, Name: call.Name, Content: s.results[call.Name]})
	}
	return out
}

func newToolHarness(t *testing.T, client *fake.Client, tools coach.ToolRunner) harness {
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
		Tools:          tools,
		// Every tier resolves to the one fake provider: these tests are about
		// the tool loop, not about which provider serves which user.
		Chains:    ai.NewChainSet([]string{client.Name()}, nil),
		Model:     "test-model",
		FastModel: "test-fast-model",
	})

	return harness{coach: svc, convos: convos, client: client, user: user, pool: pool}
}

func newConversation(t *testing.T, h harness) uuid.UUID {
	t.Helper()

	conversation, err := h.convos.Start(context.Background(), h.user.ID)
	if err != nil {
		t.Fatalf("create conversation: %v", err)
	}
	return conversation.ID
}

var searchTool = ai.Tool{
	Name:        "search_exercises",
	Description: "Find exercises.",
	Parameters:  ai.Object("arguments", map[string]*ai.Schema{"muscle": ai.String("a muscle group")}, "muscle"),
}

// The loop: the model asks, the tool runs, the answer comes back, the model
// replies using it. Only the prose reaches the person.
func TestTheCoachRunsAToolAndAnswersFromItsResult(t *testing.T) {
	t.Parallel()

	tools := &stubTools{
		tools:   []ai.Tool{searchTool},
		results: map[string]string{"search_exercises": "- barbell-deadlift — Barbell Deadlift [hamstrings]"},
	}

	client := &fake.Client{Responses: []fake.Response{
		fake.Calling(fake.ToolCall("search_exercises", `{"muscle":"hamstrings"}`)),
		{Text: "Try the barbell deadlift."},
	}}

	h := newToolHarness(t, client, tools)
	conversationID := newConversation(t, h)

	stream, err := h.coach.SendMessage(context.Background(), h.user, conversationID, "what should I do for hamstrings?")
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	reply, err := drain(stream)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}

	if !strings.Contains(reply, "barbell deadlift") {
		t.Errorf("reply = %q, want the answer that followed the tool call", reply)
	}
	// The person asked a question, not to watch the machinery.
	if strings.Contains(reply, "search_exercises") {
		t.Errorf("the tool call leaked into the reply: %q", reply)
	}

	if len(tools.calls) != 1 || len(tools.calls[0]) != 1 {
		t.Fatalf("tool calls = %v, want exactly one", tools.calls)
	}
	if tools.calls[0][0].Name != "search_exercises" {
		t.Errorf("ran %q", tools.calls[0][0].Name)
	}
	if tools.userID != h.user.ID {
		t.Errorf("tools ran as %v, want the authenticated %v", tools.userID, h.user.ID)
	}
}

// The reply that gets stored has to be the prose, not the round-trip. A
// conversation whose history disagrees with what the person saw is worse than
// one that saved nothing.
func TestOnlyTheFinalAnswerIsShownToTheReader(t *testing.T) {
	t.Parallel()

	tools := &stubTools{
		tools:   []ai.Tool{searchTool},
		results: map[string]string{"search_exercises": "- barbell-deadlift"},
	}
	client := &fake.Client{Responses: []fake.Response{
		fake.Calling(fake.ToolCall("search_exercises", `{"muscle":"hamstrings"}`)),
		{Text: "Try the barbell deadlift."},
	}}

	h := newToolHarness(t, client, tools)
	conversationID := newConversation(t, h)

	stream, err := h.coach.SendMessage(context.Background(), h.user, conversationID, "hamstrings?")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, drainErr := drain(stream); drainErr != nil {
		t.Fatalf("drain: %v", drainErr)
	}

	saved := waitForReply(t, h, conversationID, 2*time.Second)
	if !strings.Contains(saved.Content, "barbell deadlift") {
		t.Errorf("saved reply = %q", saved.Content)
	}

	// Tool turns are stored now — a paused turn has to be rebuildable — so the
	// property worth pinning is no longer "nothing else is saved" but "nothing
	// else is shown". The intermediate round must not reach the reader as a
	// second bubble.
	history, err := h.convos.History(context.Background(), conversationID)
	if err != nil {
		t.Fatalf("history: %v", err)
	}

	var visible int
	for _, m := range history {
		if m.IsModel() && !m.IsToolTurn() {
			visible++
		}
	}
	if visible != 1 {
		t.Errorf("model turns visible to the reader = %d, want 1", visible)
	}
}

// The tools go to the provider, or the model has no way to know they exist.
func TestToolsAreDeclaredToTheProvider(t *testing.T) {
	t.Parallel()

	tools := &stubTools{tools: []ai.Tool{searchTool}, results: map[string]string{}}
	client := &fake.Client{Responses: []fake.Response{{Text: "Sure."}}}

	h := newToolHarness(t, client, tools)
	conversationID := newConversation(t, h)

	stream, err := h.coach.SendMessage(context.Background(), h.user, conversationID, "hello")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, drainErr := drain(stream); drainErr != nil {
		t.Fatalf("drain: %v", drainErr)
	}

	calls := client.Calls()
	if len(calls) == 0 {
		t.Fatal("the provider was never called")
	}
	if len(calls[0].Tools) != 1 || calls[0].Tools[0].Name != "search_exercises" {
		t.Errorf("Tools = %+v, want the registry's declarations", calls[0].Tools)
	}
}

// A model that keeps asking is an unbounded bill. The loop stops, and what was
// produced is kept rather than discarded.
func TestTheToolLoopIsBounded(t *testing.T) {
	t.Parallel()

	tools := &stubTools{
		tools:   []ai.Tool{searchTool},
		results: map[string]string{"search_exercises": "- something"},
	}

	// Always asks, never answers.
	client := &fake.Client{Handler: func(context.Context, ai.Request) (fake.Response, error) {
		return fake.Calling(fake.ToolCall("search_exercises", `{"muscle":"lats"}`)), nil
	}}

	h := newToolHarness(t, client, tools)
	conversationID := newConversation(t, h)

	stream, err := h.coach.SendMessage(context.Background(), h.user, conversationID, "go forever")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, drainErr := drain(stream); drainErr != nil {
		t.Fatalf("drain: %v", drainErr)
	}

	// Bounded, and low. The exact cap is coach's business; that it exists is
	// this test's.
	if len(tools.calls) == 0 {
		t.Fatal("no tools ran at all")
	}
	if len(tools.calls) > 6 {
		t.Errorf("ran %d rounds of tools; the loop is not bounded", len(tools.calls))
	}
}

// A nil registry is the pre-tools behaviour, and every existing test builds
// the coach that way.
func TestTheCoachWorksWithNoToolsWired(t *testing.T) {
	t.Parallel()

	client := &fake.Client{Responses: []fake.Response{{Text: "Hello."}}}

	h := newToolHarness(t, client, nil)
	conversationID := newConversation(t, h)

	stream, err := h.coach.SendMessage(context.Background(), h.user, conversationID, "hello")
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	reply, err := drain(stream)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if !strings.Contains(reply, "Hello.") {
		t.Errorf("reply = %q", reply)
	}
	if calls := client.Calls(); len(calls) == 0 || calls[0].Tools != nil {
		t.Error("no tools should be declared when none are wired")
	}
}

// Both turns, in order: every provider rejects a result whose call it has not
// been shown.
func TestTheCallAndItsResultAreBothSentBack(t *testing.T) {
	t.Parallel()

	tools := &stubTools{
		tools:   []ai.Tool{searchTool},
		results: map[string]string{"search_exercises": "- barbell-deadlift"},
	}
	client := &fake.Client{Responses: []fake.Response{
		fake.Calling(fake.ToolCall("search_exercises", `{"muscle":"hamstrings"}`)),
		{Text: "Try the barbell deadlift."},
	}}

	h := newToolHarness(t, client, tools)
	conversationID := newConversation(t, h)

	stream, err := h.coach.SendMessage(context.Background(), h.user, conversationID, "hamstrings?")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, drainErr := drain(stream); drainErr != nil {
		t.Fatalf("drain: %v", drainErr)
	}

	calls := client.Calls()
	if len(calls) < 2 {
		t.Fatalf("the model was called %d times, want a second pass after the tools", len(calls))
	}

	messages := calls[1].Messages
	if len(messages) < 2 {
		t.Fatalf("the follow-up carried %d messages", len(messages))
	}

	callTurn := messages[len(messages)-2]
	resultTurn := messages[len(messages)-1]

	if !callTurn.HasToolCalls() {
		t.Error("the turn before the results is not the model's tool call")
	}
	if len(resultTurn.ToolResults) != 1 {
		t.Fatalf("the last turn carries %d results, want 1", len(resultTurn.ToolResults))
	}
	if resultTurn.ToolResults[0].ID != callTurn.ToolCalls[0].ID {
		t.Error("the result's id does not match its call, so a provider would reject it")
	}
}

// The tool turns have to reach the database, not just the in-memory request.
//
// A turn that pauses for approval ends its stream; when the person answers, the
// conversation is rebuilt from stored rows. Anything held only in memory is
// gone by then, and the provider would reject a result whose call it was never
// shown.
func TestToolTurnsArePersisted(t *testing.T) {
	t.Parallel()

	tools := &stubTools{
		tools:   []ai.Tool{searchTool},
		results: map[string]string{"search_exercises": "- barbell-deadlift"},
	}
	client := &fake.Client{Responses: []fake.Response{
		fake.Calling(fake.ToolCall("search_exercises", `{"muscle":"hamstrings"}`)),
		{Text: "Try the barbell deadlift."},
	}}

	h := newToolHarness(t, client, tools)
	conversationID := newConversation(t, h)

	stream, err := h.coach.SendMessage(context.Background(), h.user, conversationID, "hamstrings?")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, drainErr := drain(stream); drainErr != nil {
		t.Fatalf("drain: %v", drainErr)
	}
	waitForReply(t, h, conversationID, 2*time.Second)

	history, err := h.convos.History(context.Background(), conversationID)
	if err != nil {
		t.Fatalf("history: %v", err)
	}

	var calls, results int
	for _, m := range history {
		calls += len(m.ToolCalls)
		results += len(m.ToolResults)
	}

	if calls != 1 {
		t.Errorf("stored tool calls = %d, want 1", calls)
	}
	if results != 1 {
		t.Errorf("stored tool results = %d, want 1", results)
	}

	// And rebuilding the request from those rows has to produce the pair the
	// provider needs, in that order.
	rebuilt := conversations.ToAIMessages(history)
	var sawCall, sawResultAfterCall bool
	for _, m := range rebuilt {
		if len(m.ToolCalls) > 0 {
			sawCall = true
		}
		if len(m.ToolResults) > 0 && sawCall {
			sawResultAfterCall = true
		}
	}
	if !sawResultAfterCall {
		t.Error("a rebuilt request did not carry the call followed by its result")
	}
}
