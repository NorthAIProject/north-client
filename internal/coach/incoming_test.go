package coach_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/conversations"
)

type stubImages struct {
	mime string
	data []byte
	id   uuid.UUID
}

func (s stubImages) LoadInline(_ context.Context, _, mediaID uuid.UUID) (string, []byte, error) {
	if s.id != uuid.Nil && mediaID != s.id {
		return "", nil, nil
	}
	return s.mime, s.data, nil
}

func TestSendIncomingShowsThePhotoToTheModel(t *testing.T) {
	photo := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10}
	mediaID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	client := &fake.Client{Responses: []fake.Response{{Text: "Knees look fine."}}}

	h := newHarness(t, client)
	// Rebuild the service with a loader; the default harness has none.
	registry := ai.NewRegistry()
	registry.Register(client)
	h.coach = coach.NewService(coach.Options{
		Registry:       registry,
		Conversations:  h.convos,
		ContextBuilder: coach.NewContextBuilder(h.convos),
		PromptBuilder:  coach.NewPromptBuilder(),
		Chains:         ai.NewChainSet([]string{client.Name()}, nil),
		Attachments: stubImages{
			mime: "image/jpeg",
			data: photo,
			id:   mediaID,
		},
		Model:     "test-model",
		FastModel: "test-fast-model",
	})

	ctx := context.Background()
	conversation, err := h.convos.Start(ctx, h.user.ID)
	if err != nil {
		t.Fatal(err)
	}

	stream, err := h.coach.SendIncoming(ctx, h.user, conversation.ID, coach.Incoming{
		Text: "how's my squat?",
		Attachments: []conversations.Attachment{{
			MediaID:  mediaID,
			Kind:     "image",
			MIMEType: "image/jpeg",
			Name:     "squat.jpg",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := drain(stream); err != nil {
		t.Fatal(err)
	}

	var saw []byte
	for _, req := range client.Calls() {
		for _, m := range req.Messages {
			if m.Role != ai.RoleUser {
				continue
			}
			for _, p := range m.Parts {
				if len(p.InlineData) > 0 {
					saw = p.InlineData
				}
			}
		}
	}
	if string(saw) != string(photo) {
		t.Fatalf("the model was not shown the photo (inline %d bytes)", len(saw))
	}
}

func TestSendIncomingAcceptsAPhotoWithNoCaption(t *testing.T) {
	client := &fake.Client{Responses: []fake.Response{{Text: "Got it."}}}
	h := newHarness(t, client)
	registry := ai.NewRegistry()
	registry.Register(client)
	h.coach = coach.NewService(coach.Options{
		Registry:       registry,
		Conversations:  h.convos,
		ContextBuilder: coach.NewContextBuilder(h.convos),
		PromptBuilder:  coach.NewPromptBuilder(),
		Chains:         ai.NewChainSet([]string{client.Name()}, nil),
		Attachments:    stubImages{mime: "image/jpeg", data: []byte{0xff, 0xd8, 0xff}},
		Model:          "test-model",
		FastModel:      "test-fast-model",
	})

	ctx := context.Background()
	conversation, err := h.convos.Start(ctx, h.user.ID)
	if err != nil {
		t.Fatal(err)
	}

	stream, err := h.coach.SendIncoming(ctx, h.user, conversation.ID, coach.Incoming{
		Attachments: []conversations.Attachment{{
			MediaID: uuid.New(),
			Kind:    "image",
			Name:    "progress.jpg",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := drain(stream); err != nil {
		t.Fatal(err)
	}
}

func TestSendIncomingStillRejectsAnEmptyTurn(t *testing.T) {
	h := newHarness(t, &fake.Client{})
	ctx := context.Background()
	conversation, err := h.convos.Start(ctx, h.user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.coach.SendIncoming(ctx, h.user, conversation.ID, coach.Incoming{}); err == nil {
		t.Fatal("empty turn should be refused")
	}
}
