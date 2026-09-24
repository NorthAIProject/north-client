package coach

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

var (
	contractConversation = uuid.MustParse("33333333-3333-3333-3333-333333333333")
	contractAt           = time.Date(2026, 9, 24, 7, 15, 0, 0, time.UTC)
)

func TestConversationDetailShape(t *testing.T) {
	t.Parallel()

	yes := true
	history := []conversations.Message{
		{ID: uuid.MustParse("77777777-7777-7777-7777-777777777771"), Role: ai.RoleUser, Content: "How do I do a push-up?", CreatedAt: contractAt},
		// A tool plumbing turn: the coach asking itself for the catalog entry.
		{ID: uuid.MustParse("77777777-7777-7777-7777-777777777772"), Role: ai.RoleModel, ToolCalls: []ai.ToolCall{{Name: "get_exercise"}}, CreatedAt: contractAt},
		{
			ID: uuid.MustParse("77777777-7777-7777-7777-777777777773"), Role: ai.RoleModel,
			Content:      "Hands under shoulders, body in one line, lower until your chest nearly touches.",
			EvidenceRefs: []string{"exercise:push-up", "memory:88888888-8888-8888-8888-888888888888"},
			Helpful:      &yes, CreatedAt: contractAt.Add(time.Second),
		},
	}
	apitest.AssertGolden(t, "conversation.golden.json", ConversationDetail{
		Conversation: ConversationSummary{ID: contractConversation, Title: "Push-up form", Kind: conversations.KindChat, UpdatedAt: contractAt},
		Messages:     projectMessages(history),
		PendingApproval: projectApproval(PendingCall{
			MessageID: uuid.MustParse("99999999-9999-9999-9999-999999999999"),
			Calls:     []ai.ToolCall{{Name: "log_check_in"}},
		}),
	})
}

func TestConversationListShape(t *testing.T) {
	t.Parallel()

	apitest.AssertGolden(t, "conversations.golden.json", ConversationList{Conversations: []ConversationSummary{
		{ID: contractConversation, Title: "Push-up form", Kind: conversations.KindChat, UpdatedAt: contractAt},
		{ID: uuid.MustParse("44444444-4444-4444-4444-444444444444"), Title: "Reflection", Kind: conversations.KindReflection, Ended: true, UpdatedAt: contractAt},
	}})
}

// The tool call turn is the coach talking to itself; a client that showed it
// would render an empty bubble between the question and the answer.
func TestToolPlumbingTurnsAreNotMessages(t *testing.T) {
	t.Parallel()

	got := projectMessages([]conversations.Message{
		{Role: ai.RoleUser, Content: "Log my check-in"},
		{Role: ai.RoleModel, ToolCalls: []ai.ToolCall{{Name: "log_check_in"}}},
		{Role: ai.RoleUser, ToolResults: []ai.ToolResult{{Name: "log_check_in"}}},
		{Role: ai.RoleModel, Content: "Logged."},
	})
	if len(got) != 2 || got[0].Role != "user" || got[1].Role != "coach" || got[1].Text != "Logged." {
		t.Fatalf("messages = %+v, want the question and the answer only", got)
	}
}

// Every frame is one event line and one data line of JSON. A raw newline in
// data would end the frame early and cut the reply mid-sentence.
func TestEventFramesAreSingleLineJSON(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	writeEvent(rec, http.NewResponseController(rec), EventToken, TokenEvent{Text: "one\ntwo"})

	want := "event: token\ndata: {\"text\":\"one\\ntwo\"}\n\n"
	if rec.Body.String() != want {
		t.Fatalf("frame = %q, want %q", rec.Body.String(), want)
	}
}
