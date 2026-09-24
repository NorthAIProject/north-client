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

// A refusal never reaches the registry — the tool is not invoked at all — so
// the coach has to report it itself, or the log would show only the writes
// that happened and none of the ones somebody stopped.
func TestADeclinedWriteIsReportedToTheAudit(t *testing.T) {
	t.Parallel()

	tools := &stubTools{
		tools:    []ai.Tool{writeTool},
		results:  map[string]string{"create_check_in": "logged"},
		readOnly: map[string]bool{"create_check_in": false},
	}
	client := &fake.Client{Responses: []fake.Response{
		fake.Calling(fake.ToolCall("create_check_in", `{"mood":4}`)),
		{Text: "Nothing recorded."},
	}}

	audit := &stubAudit{}
	h := newToolHarnessWithAudit(t, client, tools, audit)
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

	if len(audit.declined) != 1 {
		t.Fatalf("declined records = %d, want 1", len(audit.declined))
	}
	if audit.declined[0].Tool != "create_check_in" {
		t.Errorf("recorded %q", audit.declined[0].Tool)
	}
	if audit.declined[0].UserID != h.user.ID {
		t.Error("the decline was recorded against the wrong account")
	}
}

// Approving is recorded by the registry, not here — recording it twice would
// make one write look like two.
func TestAnApprovedWriteIsNotDoubleReported(t *testing.T) {
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

	audit := &stubAudit{}
	h := newToolHarnessWithAudit(t, client, tools, audit)
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

	if len(audit.declined) != 0 {
		t.Errorf("an approved write was recorded as declined %d times", len(audit.declined))
	}
}

type stubAudit struct{ declined []coach.DeclinedCall }

func (s *stubAudit) RecordDeclinedCall(_ context.Context, c coach.DeclinedCall) {
	s.declined = append(s.declined, c)
}

// fillThread gives a conversation more history than one page of it holds.
//
// A linked Telegram chat is a single thread that runs for weeks, and anything
// reading "the latest turn" off a first page of it reads a turn from the
// start of the conversation instead.
func fillThread(t *testing.T, h harness, conversationID uuid.UUID, pairs int) {
	t.Helper()

	ctx := context.Background()
	for i := range pairs {
		if _, err := h.convos.AppendUserMessage(ctx, conversationID, "morning", nil); err != nil {
			t.Fatalf("seed user message %d: %v", i, err)
		}
		if _, err := h.convos.AppendModelMessage(ctx, conversationID, "morning to you",
			nil, "test-model", "fake", nil, conversations.Provenance{}); err != nil {
			t.Fatalf("seed model message %d: %v", i, err)
		}
	}
}

// Added after Telegram sent "show me how to do a squat" into a thread of
// several hundred messages: get_exercise ran and the reply went out without
// its animation, because the lookup was read back off the oldest page of the
// thread rather than off the reply that made it.
func TestTheLatestExerciseIsFoundOnALongThread(t *testing.T) {
	t.Parallel()

	tools := &stubTools{
		tools:    []ai.Tool{{Name: coach.ToolGetExercise, Description: "Read one exercise."}},
		results:  map[string]string{coach.ToolGetExercise: "Squat (strength, beginner)"},
		readOnly: map[string]bool{coach.ToolGetExercise: true},
	}
	client := &fake.Client{Responses: []fake.Response{
		fake.Calling(fake.ToolCall(coach.ToolGetExercise, `{"slug":"squat"}`)),
		{Text: "Feet shoulder-width, sit back and down."},
	}}

	h := newToolHarness(t, client, tools)
	conversationID := newConversation(t, h)
	fillThread(t, h, conversationID, 150)

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

// The same page, read for the same reason: a write suspended at the end of a
// long thread must still be found, or the person is never asked to approve it.
func TestAWriteSuspendedOnALongThreadIsStillPending(t *testing.T) {
	t.Parallel()

	tools := &stubTools{
		tools:    []ai.Tool{writeTool},
		results:  map[string]string{"create_check_in": "logged"},
		readOnly: map[string]bool{"create_check_in": false},
	}
	client := &fake.Client{Responses: []fake.Response{
		fake.Calling(fake.ToolCall("create_check_in", `{"mood":4}`)),
	}}

	h := newToolHarness(t, client, tools)
	conversationID := newConversation(t, h)
	fillThread(t, h, conversationID, 150)

	stream, err := h.coach.SendMessage(context.Background(), h.user, conversationID, "log my check-in")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if _, drainErr := drain(stream); drainErr != nil {
		t.Fatalf("drain: %v", drainErr)
	}

	_, ok, err := h.coach.PendingApproval(context.Background(), h.user, conversationID)
	if err != nil {
		t.Fatalf("pending approval: %v", err)
	}
	if !ok {
		t.Error("nothing is awaiting approval; the write at the end of a long thread was missed")
	}
}
