package coach_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/users"
)

// writeTool is a capability that changes something. The loop must not run one
// without being told to.
var writeTool = ai.Tool{
	Name:        "create_check_in",
	Description: "Record today's check-in.",
	Parameters:  ai.Object("today's check-in", map[string]*ai.Schema{"mood": ai.Integer("1 to 5")}, "mood"),
}

// A write must not run just because a model asked for it.
//
// This is the whole of NOR-30: until now ReadOnly was an annotation MCP clients
// read and the coach ignored, so a model could write to somebody's record
// mid-sentence with nothing in the way.
func TestAWriteToolIsNotRunWithoutApproval(t *testing.T) {
	t.Parallel()

	tools := &stubTools{
		tools:    []ai.Tool{writeTool},
		results:  map[string]string{"create_check_in": "logged"},
		readOnly: map[string]bool{"create_check_in": false},
	}
	client := &fake.Client{Responses: []fake.Response{
		fake.Calling(fake.ToolCall("create_check_in", `{"mood":4}`)),
		{Text: "Logged it."},
	}}

	h := newToolHarness(t, client, tools)
	conversationID := newConversation(t, h)

	stream, err := h.coach.SendMessage(context.Background(), h.user, conversationID, "log my check-in")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, drainErr := drain(stream); drainErr != nil {
		t.Fatalf("drain: %v", drainErr)
	}

	if len(tools.calls) != 0 {
		t.Errorf("the write ran %d times without approval", len(tools.calls))
	}
}

// Suspending is not the same as forgetting. The call has to be on disk, or
// there is nothing to approve once the stream has closed.
func TestASuspendedWriteIsLeftPendingOnTheConversation(t *testing.T) {
	t.Parallel()

	tools := &stubTools{
		tools:    []ai.Tool{writeTool},
		results:  map[string]string{"create_check_in": "logged"},
		readOnly: map[string]bool{"create_check_in": false},
	}
	client := &fake.Client{Responses: []fake.Response{
		fake.Calling(fake.ToolCall("create_check_in", `{"mood":4}`)),
		{Text: "Logged it."},
	}}

	h := newToolHarness(t, client, tools)
	conversationID := newConversation(t, h)

	stream, err := h.coach.SendMessage(context.Background(), h.user, conversationID, "log my check-in")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, drainErr := drain(stream); drainErr != nil {
		t.Fatalf("drain: %v", drainErr)
	}

	pending, ok, err := h.coach.PendingApproval(context.Background(), h.user, conversationID)
	if err != nil {
		t.Fatalf("pending approval: %v", err)
	}
	if !ok {
		t.Fatal("nothing is awaiting approval; the suspended call was not recorded")
	}
	if len(pending.Calls) != 1 || pending.Calls[0].Name != "create_check_in" {
		t.Errorf("pending calls = %+v, want the one create_check_in", pending.Calls)
	}
}

// Read-only tools are the common case and must not gain a round trip.
func TestAReadOnlyToolStillRunsWithoutAsking(t *testing.T) {
	t.Parallel()

	tools := &stubTools{
		tools:    []ai.Tool{searchTool},
		results:  map[string]string{"search_exercises": "- barbell-deadlift"},
		readOnly: map[string]bool{"search_exercises": true},
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
	text, drainErr := drain(stream)
	if drainErr != nil {
		t.Fatalf("drain: %v", drainErr)
	}

	if len(tools.calls) != 1 {
		t.Errorf("the read-only tool ran %d times, want 1 — it should not need approval", len(tools.calls))
	}
	if !strings.Contains(text, "barbell deadlift") {
		t.Errorf("reply = %q, want the answer built from the tool result", text)
	}

	if _, ok, err := h.coach.PendingApproval(context.Background(), h.user, conversationID); err != nil {
		t.Fatalf("pending approval: %v", err)
	} else if ok {
		t.Error("a read-only call was left awaiting approval")
	}
}

// Approving runs the tool once and lets the model finish the sentence it
// started.
func TestApprovingRunsTheToolAndResumesTheReply(t *testing.T) {
	t.Parallel()

	tools := &stubTools{
		tools:    []ai.Tool{writeTool},
		results:  map[string]string{"create_check_in": "Logged: mood 4. That is a 5-day streak."},
		readOnly: map[string]bool{"create_check_in": false},
	}
	client := &fake.Client{Responses: []fake.Response{
		fake.Calling(fake.ToolCall("create_check_in", `{"mood":4}`)),
		{Text: "Logged it — that is a 5-day streak."},
	}}

	h := newToolHarness(t, client, tools)
	conversationID := newConversation(t, h)

	stream, err := h.coach.SendMessage(context.Background(), h.user, conversationID, "log my check-in")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, drainErr := drain(stream); drainErr != nil {
		t.Fatalf("drain: %v", drainErr)
	}

	if err = h.coach.ResolvePending(context.Background(), h.user, conversationID, uuid.Nil, true); err != nil {
		t.Fatalf("approve: %v", err)
	}
	resumed, err := h.coach.Resume(context.Background(), h.user, conversationID)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	text, drainErr := drain(resumed)
	if drainErr != nil {
		t.Fatalf("drain resumed: %v", drainErr)
	}

	if len(tools.calls) != 1 {
		t.Fatalf("the tool ran %d times, want exactly 1", len(tools.calls))
	}
	if !strings.Contains(text, "streak") {
		t.Errorf("resumed reply = %q, want the answer built from the tool result", text)
	}

	if _, ok, err := h.coach.PendingApproval(context.Background(), h.user, conversationID); err != nil {
		t.Fatalf("pending approval: %v", err)
	} else if ok {
		t.Error("the call is still pending after being approved")
	}

	waitForReply(t, h, conversationID, 2*time.Second)
}

// Declining must tell the model, not leave it waiting. A refusal it never hears
// about is one it will narrate as though it succeeded.
func TestDecliningNeverRunsTheToolAndSaysSo(t *testing.T) {
	t.Parallel()

	tools := &stubTools{
		tools:    []ai.Tool{writeTool},
		results:  map[string]string{"create_check_in": "logged"},
		readOnly: map[string]bool{"create_check_in": false},
	}
	client := &fake.Client{Responses: []fake.Response{
		fake.Calling(fake.ToolCall("create_check_in", `{"mood":4}`)),
		{Text: "No problem, I have not recorded anything."},
	}}

	h := newToolHarness(t, client, tools)
	conversationID := newConversation(t, h)

	stream, err := h.coach.SendMessage(context.Background(), h.user, conversationID, "log my check-in")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, drainErr := drain(stream); drainErr != nil {
		t.Fatalf("drain: %v", drainErr)
	}

	if err = h.coach.ResolvePending(context.Background(), h.user, conversationID, uuid.Nil, false); err != nil {
		t.Fatalf("decline: %v", err)
	}
	resumed, err := h.coach.Resume(context.Background(), h.user, conversationID)
	if err != nil {
		t.Fatalf("resume: %v", err)
	}
	if _, drainErr := drain(resumed); drainErr != nil {
		t.Fatalf("drain resumed: %v", drainErr)
	}

	if len(tools.calls) != 0 {
		t.Errorf("the tool ran %d times after being declined", len(tools.calls))
	}

	history, err := h.convos.History(context.Background(), conversationID)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	var refusal string
	for _, m := range history {
		for _, r := range m.ToolResults {
			refusal = r.Content
		}
	}
	if !strings.Contains(strings.ToLower(refusal), "declin") {
		t.Errorf("the recorded result was %q; the model was not told it was refused", refusal)
	}
}

// Approving twice must not write twice. The card is a button on a page, and a
// page can be submitted twice.
func TestResolvingAnAlreadyResolvedCallIsRefused(t *testing.T) {
	t.Parallel()

	tools := &stubTools{
		tools:    []ai.Tool{writeTool},
		results:  map[string]string{"create_check_in": "logged"},
		readOnly: map[string]bool{"create_check_in": false},
	}
	client := &fake.Client{Responses: []fake.Response{
		fake.Calling(fake.ToolCall("create_check_in", `{"mood":4}`)),
		{Text: "Logged it."},
		{Text: "Already done."},
	}}

	h := newToolHarness(t, client, tools)
	conversationID := newConversation(t, h)

	stream, err := h.coach.SendMessage(context.Background(), h.user, conversationID, "log my check-in")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, drainErr := drain(stream); drainErr != nil {
		t.Fatalf("drain: %v", drainErr)
	}

	if err = h.coach.ResolvePending(context.Background(), h.user, conversationID, uuid.Nil, true); err != nil {
		t.Fatalf("first approve: %v", err)
	}

	if err = h.coach.ResolvePending(context.Background(), h.user, conversationID, uuid.Nil, true); err == nil {
		t.Fatal("approving twice was allowed; a double submit would write twice")
	}
	if len(tools.calls) != 1 {
		t.Errorf("the tool ran %d times, want exactly 1", len(tools.calls))
	}
}

// A pending call belongs to one conversation and one account.
func TestPendingApprovalIsScopedToTheOwner(t *testing.T) {
	t.Parallel()

	tools := &stubTools{
		tools:    []ai.Tool{writeTool},
		results:  map[string]string{"create_check_in": "logged"},
		readOnly: map[string]bool{"create_check_in": false},
	}
	client := &fake.Client{Responses: []fake.Response{
		fake.Calling(fake.ToolCall("create_check_in", `{"mood":4}`)),
		{Text: "Logged it."},
	}}

	h := newToolHarness(t, client, tools)
	conversationID := newConversation(t, h)

	stream, err := h.coach.SendMessage(context.Background(), h.user, conversationID, "log my check-in")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, drainErr := drain(stream); drainErr != nil {
		t.Fatalf("drain: %v", drainErr)
	}

	stranger := registerStranger(t, h)

	if _, _, err := h.coach.PendingApproval(context.Background(), stranger, conversationID); err == nil {
		t.Error("another account could read the pending call")
	}
	if err := h.coach.ResolvePending(context.Background(), stranger, conversationID, uuid.Nil, true); err == nil {
		t.Error("another account could approve the write")
	}
	if len(tools.calls) != 0 {
		t.Error("a stranger's approval ran the tool")
	}
}

// registerStranger makes a second account on the same pool, so the ownership
// checks are exercised against a real user rather than a random uuid.
func registerStranger(t *testing.T, h harness) users.User {
	t.Helper()

	user, err := users.NewService(users.NewRepository(h.pool)).Register(context.Background(), users.Registration{
		Email:        "stranger@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Stranger",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("register stranger: %v", err)
	}
	return user
}
