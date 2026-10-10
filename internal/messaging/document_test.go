package messaging_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/messaging"
)

// stubAttachments stands in for the media service's chat-attachment store,
// recording what it was handed.
type stubAttachments struct {
	names []string
	bytes [][]byte
}

func (s *stubAttachments) StoreChatAttachment(_ context.Context, _ uuid.UUID, filename string, _ int64, body io.Reader) (conversations.Attachment, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return conversations.Attachment{}, err
	}
	s.names = append(s.names, filename)
	s.bytes = append(s.bytes, data)
	return conversations.Attachment{MediaID: uuid.New(), Kind: "file", MIMEType: "application/pdf", Name: filename}, nil
}

func (h harness) sendDocument(t *testing.T, chat, caption string, file messaging.InboundFile) messaging.OutboundMessage {
	t.Helper()
	out, err := h.messaging.Handle(context.Background(), messaging.InboundMessage{
		Platform:   messaging.PlatformTelegram,
		ExternalID: chat,
		Text:       caption,
		UpdateID:   nextUpdateID(),
		ReceivedAt: time.Now(),
		Attachment: &file,
	})
	if err != nil {
		t.Fatalf("handle a document: %v", err)
	}
	return out
}

func pdf() []byte { return []byte("%PDF-1.7 pretend this is a diet plan") }

// A diet PDF sent to the bot is the same turn as one attached in the app: the
// file is stored as a chat attachment and the caption is the words.
func TestATelegramDocumentReachesTheCoachAsAFile(t *testing.T) {
	client := fake.Text("I can import that.")
	stored := &stubAttachments{}
	h := newHarness(t, client, harnessOptions{stored: stored})
	h.link(t, "700101")

	out := h.sendDocument(t, "700101", "import my diet", messaging.InboundFile{
		Kind: messaging.KindFile, Name: "dieta.pdf", MIMEType: "application/pdf",
		SizeBytes: int64(len(pdf())), FileID: "BQACAgQAAx", Bytes: pdf(),
	})
	if out.Text != "I can import that." {
		t.Fatalf("reply = %q", out.Text)
	}
	if len(stored.names) != 1 || stored.names[0] != "dieta.pdf" || string(stored.bytes[0]) != string(pdf()) {
		t.Fatalf("stored %v, want dieta.pdf with its bytes", stored.names)
	}

	threads, err := h.convos.List(context.Background(), h.user.ID, 1)
	if err != nil || len(threads) != 1 {
		t.Fatalf("threads = %d, %v", len(threads), err)
	}
	history, err := h.convos.History(context.Background(), threads[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	var sent *conversations.Message
	for i := range history {
		if history[i].IsUser() {
			sent = &history[i]
		}
	}
	if sent == nil {
		t.Fatal("no user message was stored")
	}
	if sent.Content != "import my diet" {
		t.Errorf("the turn's words = %q, want the caption", sent.Content)
	}
	if len(sent.Parts) != 1 || sent.Parts[0].Kind != "file" || sent.Parts[0].Name != "dieta.pdf" {
		t.Errorf("the turn's attachments = %+v, want one file named dieta.pdf", sent.Parts)
	}
	if len(client.Calls()) == 0 {
		t.Error("the coach was never called")
	}
}

// A file the coach cannot read, or one too big to store, is answered in the
// chat with what it does take — and never spends a coach turn.
func TestARefusedDocumentNeverReachesTheCoach(t *testing.T) {
	for name, file := range map[string]messaging.InboundFile{
		"too big": {
			Kind: messaging.KindFile, Name: "dieta.pdf", MIMEType: "application/pdf",
			SizeBytes: 9 << 20, FileID: "BQACAgQAAx",
		},
		"unsupported": {
			Kind: messaging.KindFile, Name: "photos.zip", MIMEType: "application/zip",
			SizeBytes: 1 << 10, FileID: "BQACAgQAAy", Bytes: []byte("PK\x03\x04"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			client := fake.Text("should not be said")
			stored := &stubAttachments{}
			h := newHarness(t, client, harnessOptions{stored: stored})
			h.link(t, "700102")

			out := h.sendDocument(t, "700102", "import this", file)
			if !strings.Contains(out.Text, "PDF") || !strings.Contains(out.Text, "8 MB") {
				t.Errorf("refusal = %q, want the accepted types and size", out.Text)
			}
			if len(stored.names) != 0 {
				t.Errorf("a refused file was stored: %v", stored.names)
			}
			if len(client.Calls()) != 0 {
				t.Errorf("the coach was called %d times for a refused file", len(client.Calls()))
			}
		})
	}
}
