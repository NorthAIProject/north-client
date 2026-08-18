package conversations_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/conversations"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func TestValidateTurnAllowsAPhotoWithNoCaption(t *testing.T) {
	if err := conversations.ValidateTurn("", true); err != nil {
		t.Fatalf("a photo with no caption is a complete turn: %v", err)
	}
}

func TestValidateTurnStillRejectsAnEmptyBox(t *testing.T) {
	err := conversations.ValidateTurn("   ", false)
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("got %v, want validation", err)
	}
}

func TestToAIMessagesKeepsAPhotoOnlyTurnAsANote(t *testing.T) {
	out := conversations.ToAIMessages([]conversations.Message{
		{
			Role: ai.RoleUser,
			Parts: []conversations.Attachment{{
				MediaID: uuid.MustParse("11111111-1111-1111-1111-111111111111"),
				Kind:    "image",
				Name:    "squat.jpg",
			}},
		},
		{Role: ai.RoleModel, Content: "Knees are tracking well."},
	})

	if len(out) != 2 {
		t.Fatalf("messages = %d, want 2 — the photo turn must not be dropped", len(out))
	}
	if got := out[0].Text(); !strings.Contains(got, "squat.jpg") {
		t.Errorf("photo turn = %q, want the filename in the note", got)
	}
	if strings.Contains(out[0].Text(), "11111111") {
		t.Error("the media id does not belong in the prompt")
	}
}

func TestToAIMessagesDoesNotInlinePastBytes(t *testing.T) {
	out := conversations.ToAIMessages([]conversations.Message{{
		Role:    ai.RoleUser,
		Content: "how's my form?",
		Parts: []conversations.Attachment{{
			MediaID:  uuid.New(),
			Kind:     "image",
			MIMEType: "image/jpeg",
			Name:     "set.jpg",
		}},
	}})

	if len(out) != 1 {
		t.Fatalf("messages = %d", len(out))
	}
	for _, p := range out[0].Parts {
		if len(p.InlineData) > 0 || p.FileURI != "" {
			t.Fatal("history must not carry file bytes; the current turn is hydrated separately")
		}
	}
}
