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

func TestComposerAcceptsAPhoto(t *testing.T) {
	var buf bytes.Buffer
	err := Page(
		users.User{DisplayName: "Fernando"},
		conversations.Conversation{ID: uuid.MustParse("11111111-1111-1111-1111-111111111111")},
		nil,
		nil,
		CoachStats{},
		nil,
		false,
		"",
	).Render(context.Background(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	for _, want := range []string{
		`hx-encoding="multipart/form-data"`,
		`name="attachment"`,
		`accept="image/jpeg,image/png,image/webp,image/gif"`,
		`aria-label="Attach a photo"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("composer missing %q", want)
		}
	}
}

func TestBubbleRendersAPhoto(t *testing.T) {
	var buf bytes.Buffer
	err := Bubble(conversations.Message{
		Role: ai.RoleUser,
		Parts: []conversations.Attachment{{
			MediaID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
			Kind:    "image",
			Name:    "squat.jpg",
		}},
	}).Render(context.Background(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	if !strings.Contains(body, "/app/media/aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa") {
		t.Error("user bubble should show the stored photo")
	}
}

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
		"",
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

func TestEmptyOffersStarterChips(t *testing.T) {
	var buf bytes.Buffer
	if err := Empty(users.User{DisplayName: "Ada"}, nil, CoachStats{}).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	body := buf.String()
	if !strings.Contains(body, "I want to get stronger") {
		t.Fatal("empty chat has no first-message chips")
	}
	if !strings.Contains(body, `name="draft"`) {
		t.Fatal("chips do not submit a draft")
	}
}

func TestStreamingBubbleKeepsACaret(t *testing.T) {
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")

	var pending bytes.Buffer
	if err := PendingExchange(id, "How did training go?", uuid.Nil).Render(context.Background(), &pending); err != nil {
		t.Fatal(err)
	}
	var resume bytes.Buffer
	if err := ResumeExchange(id).Render(context.Background(), &resume); err != nil {
		t.Fatal(err)
	}

	for name, body := range map[string]string{
		"pending": pending.String(),
		"resume":  resume.String(),
	} {
		if !strings.Contains(body, `data-streaming-caret`) {
			t.Errorf("%s stream has no caret", name)
		}
		if !strings.Contains(body, "motion-safe:animate-caret") {
			t.Errorf("%s caret is not the CLI blink", name)
		}
		if !strings.Contains(body, `sse-swap="token,error"`) {
			t.Errorf("%s lost the token target", name)
		}
	}
}

func TestComposerPrefillsADraft(t *testing.T) {
	var buf bytes.Buffer
	err := Page(
		users.User{DisplayName: "Ada"},
		conversations.Conversation{ID: uuid.MustParse("11111111-1111-1111-1111-111111111111")},
		nil,
		nil,
		CoachStats{},
		nil,
		false,
		"Help me build a habit I'll actually keep.",
	).Render(context.Background(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Help me build a habit I&#39;ll actually keep.") &&
		!strings.Contains(buf.String(), "Help me build a habit I'll actually keep.") {
		t.Fatal("composer did not prefill the draft")
	}
}
