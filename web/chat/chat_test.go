package chat

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/users"
)

func TestPageKeepsTheComposerReachableOnAPhone(t *testing.T) {
	var buf bytes.Buffer
	err := Page(
		users.User{DisplayName: "Fernando"},
		conversations.Conversation{ID: uuid.MustParse("11111111-1111-1111-1111-111111111111")},
		nil,
		nil,
		CoachStats{},
		nil,
		false,
	).Render(context.Background(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	for _, want := range []string{
		`id="chat-root"`,
		"visualViewport",
		"env(safe-area-inset-bottom)",
		"min-h-11 min-w-11",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("chat page missing %q", want)
		}
	}
}

func TestCopyIsVisibleOnTouch(t *testing.T) {
	var buf bytes.Buffer
	err := Bubble(conversations.Message{
		Role:    ai.RoleModel,
		Content: "A reply.",
	}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	if !strings.Contains(body, "sm:opacity-0 sm:group-hover:opacity-100") {
		t.Error("copy control is hover-only; thumbs never hover")
	}
}

func TestApprovalButtonsAreThumbSized(t *testing.T) {
	var buf bytes.Buffer
	err := ApprovalCard(uuid.MustParse("11111111-1111-1111-1111-111111111111"), []PendingTool{{
		MessageID: uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Name:      "log_check_in",
		Summary:   "log a check-in",
	}}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	if !strings.Contains(body, "min-h-11") {
		t.Error("approval buttons smaller than 44px")
	}
}
