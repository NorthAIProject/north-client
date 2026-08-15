package conversations_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

func newConversation(t *testing.T) (*conversations.Service, conversations.Conversation) {
	t.Helper()

	pool := testdb.New(t)
	ctx := context.Background()

	user, err := users.NewService(users.NewRepository(pool)).Register(ctx, users.Registration{
		Email:        "fernando@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Fernando Correia",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	svc := conversations.NewService(conversations.NewRepository(pool))
	conversation, err := svc.Start(ctx, user.ID)
	if err != nil {
		t.Fatalf("start conversation: %v", err)
	}
	return svc, conversation
}

// The whole reason these columns exist: a turn that paused for approval has to
// be rebuildable from the database after the stream that produced it is gone.
func TestAToolCallSurvivesARoundTrip(t *testing.T) {
	svc, conversation := newConversation(t)
	ctx := context.Background()

	calls := []ai.ToolCall{{
		ID:        "call_1",
		Name:      "create_check_in",
		Arguments: []byte(`{"mood":4,"energy":3}`),
	}}

	if _, err := svc.AppendToolCalls(ctx, conversation.ID, calls); err != nil {
		t.Fatalf("append tool calls: %v", err)
	}

	history, err := svc.History(ctx, conversation.ID)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("messages = %d, want 1", len(history))
	}

	stored := history[0]
	if len(stored.ToolCalls) != 1 {
		t.Fatalf("tool calls = %d, want 1", len(stored.ToolCalls))
	}
	if stored.ToolCalls[0].Name != "create_check_in" {
		t.Errorf("name = %q, want create_check_in", stored.ToolCalls[0].Name)
	}
	// Compared as JSON, not as bytes: jsonb reformats on the way in, so the
	// text that comes back is spaced differently from the text that went. What
	// has to survive is the meaning, which is all Decode reads.
	var got map[string]int
	if err := json.Unmarshal(stored.ToolCalls[0].Arguments, &got); err != nil {
		t.Fatalf("stored arguments are not JSON: %v", err)
	}
	if got["mood"] != 4 || got["energy"] != 3 {
		t.Errorf("arguments = %v, want mood 4 and energy 3", got)
	}
	if stored.Role != ai.RoleModel {
		t.Errorf("role = %q; a tool call is the model's turn", stored.Role)
	}
}

func TestAToolResultSurvivesARoundTrip(t *testing.T) {
	svc, conversation := newConversation(t)
	ctx := context.Background()

	results := []ai.ToolResult{{
		ID:      "call_1",
		Name:    "create_check_in",
		Content: "Logged today's check-in: mood 4, energy 3.",
	}}

	if _, err := svc.AppendToolResults(ctx, conversation.ID, results); err != nil {
		t.Fatalf("append tool results: %v", err)
	}

	history, err := svc.History(ctx, conversation.ID)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("messages = %d, want 1", len(history))
	}

	stored := history[0]
	if len(stored.ToolResults) != 1 {
		t.Fatalf("tool results = %d, want 1", len(stored.ToolResults))
	}
	if stored.ToolResults[0].Content != "Logged today's check-in: mood 4, energy 3." {
		t.Errorf("content = %q", stored.ToolResults[0].Content)
	}
	if stored.Role != ai.RoleUser {
		t.Errorf("role = %q; a tool result is carried back on the user's turn", stored.Role)
	}
}

// ToAIMessages used to drop any turn with blank content, because an empty turn
// carries nothing and some providers reject it. A tool call has no text, so
// that rule would silently delete exactly the turns a resumed request needs.
func TestToAIMessagesKeepsToolTurnsAndStillDropsEmptyOnes(t *testing.T) {
	messages := []conversations.Message{
		{Role: ai.RoleUser, Content: "log my check-in"},
		{Role: ai.RoleModel, ToolCalls: []ai.ToolCall{{ID: "c1", Name: "create_check_in"}}},
		{Role: ai.RoleUser, ToolResults: []ai.ToolResult{{ID: "c1", Name: "create_check_in", Content: "done"}}},
		{Role: ai.RoleModel, Content: ""},
		{Role: ai.RoleModel, Content: "Logged it."},
	}

	out := conversations.ToAIMessages(messages)

	if len(out) != 4 {
		t.Fatalf("messages = %d, want 4 — the truly empty turn dropped, the tool turns kept", len(out))
	}
	if len(out[1].ToolCalls) != 1 {
		t.Error("the tool call turn lost its calls")
	}
	if len(out[2].ToolResults) != 1 {
		t.Error("the tool result turn lost its results")
	}
	if out[3].Parts[0].Text != "Logged it." {
		t.Errorf("last message = %+v, want the final answer", out[3])
	}
}
