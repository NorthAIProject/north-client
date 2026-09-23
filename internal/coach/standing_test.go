package coach_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/agent"
	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/watches"
)

const sleepWatchArgs = `{"watch":"your sleep","notify_when":"it drops under seven hours",` +
	`"spec":"Look at last night's sleep and tell me if it was under seven hours.",` +
	`"cadence":"daily","weekday":"","time":"08:00"}`

type standingHarness struct {
	harness
	watches *watches.Service
}

// newStandingHarness wires the real capability registry with only
// create_watch in it, so approving the card runs the real tool against the
// real table.
func newStandingHarness(t *testing.T, client *fake.Client) standingHarness {
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
	watchSvc := watches.NewService(watches.NewRepository(pool), userSvc)

	svc := coach.NewService(coach.Options{
		Registry:       registry,
		Conversations:  convos,
		ContextBuilder: coach.NewContextBuilder(convos),
		PromptBuilder:  coach.NewPromptBuilder(),
		Tools:          agent.Build(agent.Services{Watches: watchSvc}),
		Chains:         ai.NewChainSet([]string{client.Name()}, nil),
		Model:          "test-model",
	})

	return standingHarness{
		harness: harness{coach: svc, convos: convos, client: client, user: user, pool: pool},
		watches: watchSvc,
	}
}

func proposeWatch(t *testing.T, h standingHarness) uuid.UUID {
	t.Helper()
	conversationID := newConversation(t, h.harness)
	stream, err := h.coach.SendMessage(context.Background(), h.user, conversationID,
		"Every morning at 8, check my sleep and tell me if it was under seven hours.")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, err = drain(stream); err != nil {
		t.Fatalf("drain: %v", err)
	}
	return conversationID
}

// create_watch writes, so it stops at the card. Confirm stores the watch in
// the thread it was asked in, and the coach answers with the contract's
// sentence under a "Standing task" caption — with no further generation.
func TestConfirmingAStandingTaskCreatesTheWatchAndConfirms(t *testing.T) {
	t.Parallel()

	client := &fake.Client{Responses: []fake.Response{
		fake.Calling(fake.ToolCall(watches.ToolName, sleepWatchArgs)),
		{Text: "this reply must never be generated"},
	}}
	h := newStandingHarness(t, client)
	conversationID := proposeWatch(t, h)
	ctx := context.Background()

	pending, ok, err := h.coach.PendingApproval(ctx, h.user, conversationID)
	if err != nil || !ok {
		t.Fatalf("pending approval: ok=%v err=%v", ok, err)
	}
	if list, _ := h.watches.List(ctx, h.user.ID); len(list) != 0 {
		t.Fatalf("a watch exists before anybody confirmed it: %+v", list)
	}

	callsBefore := len(client.Calls())
	if err = h.coach.ResolvePending(ctx, h.user, conversationID, pending.MessageID, true); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	list, err := h.watches.List(ctx, h.user.ID)
	if err != nil {
		t.Fatalf("list watches: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("watches = %d, want 1", len(list))
	}
	w := list[0]
	if w.ConversationID != conversationID {
		t.Errorf("watch posts to %s, want the thread it was set up in (%s)", w.ConversationID, conversationID)
	}
	if w.Schedule.Cadence != watches.CadenceDaily || w.Schedule.Minute != 8*60 {
		t.Errorf("schedule = %+v, want daily at 8:00", w.Schedule)
	}
	if got := w.NextRunAt.In(h.user.Location()); got.Hour() != 8 || got.Minute() != 0 {
		t.Errorf("first run at %v local, want 8:00", got)
	}

	history, err := h.convos.History(ctx, conversationID)
	if err != nil {
		t.Fatal(err)
	}
	last := history[len(history)-1]
	want := "Got it — I'll watch your sleep and ping you when it drops under seven hours."
	if last.Content != want {
		t.Errorf("confirmation = %q, want %q", last.Content, want)
	}
	if !last.IsProactive() || last.SourceLabel != conversations.SourceStandingTask {
		t.Errorf("confirmation provenance = %q/%q, want proactive/Standing task", last.Origin, last.SourceLabel)
	}
	if len(client.Calls()) != callsBefore {
		t.Error("confirming a standing task generated another model turn")
	}
	if _, stillPending, _ := h.coach.PendingApproval(ctx, h.user, conversationID); stillPending {
		t.Error("the card is still pending after confirm")
	}
}

func TestNotNowCreatesNoWatch(t *testing.T) {
	t.Parallel()

	client := &fake.Client{Responses: []fake.Response{
		fake.Calling(fake.ToolCall(watches.ToolName, sleepWatchArgs)),
		{Text: "No problem."},
	}}
	h := newStandingHarness(t, client)
	conversationID := proposeWatch(t, h)
	ctx := context.Background()

	pending, _, err := h.coach.PendingApproval(ctx, h.user, conversationID)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.coach.ResolvePending(ctx, h.user, conversationID, pending.MessageID, false); err != nil {
		t.Fatalf("decline: %v", err)
	}
	if list, _ := h.watches.List(ctx, h.user.ID); len(list) != 0 {
		t.Errorf("declining stored a watch: %+v", list)
	}
	history, _ := h.convos.History(ctx, conversationID)
	if len(history[len(history)-1].ToolResults) == 0 {
		t.Error("a declined card should leave the refusal for the model to acknowledge on resume")
	}
}

// A run is the coach speaking first: the task goes to the model as a turn the
// person never wrote, and only the answer is stored, as proactive.
func TestRunStandingTaskPostsAProactiveReply(t *testing.T) {
	t.Parallel()

	client := &fake.Client{Responses: []fake.Response{{Text: "You slept 6h10 — the shortest night this week."}}}
	h := newStandingHarness(t, client)
	conversationID := newConversation(t, h.harness)
	ctx := context.Background()

	msg, spoke, err := h.coach.RunStandingTask(ctx, h.user, conversationID, "Check my sleep against seven hours.")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !spoke {
		t.Fatal("the coach had something to say and nothing was posted")
	}
	if msg.ConversationID != conversationID {
		t.Errorf("posted to %s, want %s", msg.ConversationID, conversationID)
	}
	if !msg.IsProactive() || msg.SourceLabel != conversations.SourceStandingTask {
		t.Errorf("provenance = %q/%q", msg.Origin, msg.SourceLabel)
	}

	history, _ := h.convos.History(ctx, conversationID)
	if len(history) != 1 {
		t.Errorf("history has %d turns, want only the posted answer", len(history))
	}
	last := client.LastCall()
	if !strings.Contains(last.Messages[len(last.Messages)-1].Text(), "Check my sleep against seven hours.") {
		t.Error("the task instruction did not reach the model")
	}
}

func TestRunStandingTaskStaysQuietWithNothingToReport(t *testing.T) {
	t.Parallel()

	client := &fake.Client{Responses: []fake.Response{{Text: "NOTHING_TO_REPORT."}}}
	h := newStandingHarness(t, client)
	conversationID := newConversation(t, h.harness)
	ctx := context.Background()

	_, spoke, err := h.coach.RunStandingTask(ctx, h.user, conversationID, "Check my sleep.")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if spoke {
		t.Error("a silent run posted a message")
	}
	if history, _ := h.convos.History(ctx, conversationID); len(history) != 0 {
		t.Errorf("history = %d turns, want none", len(history))
	}
}
