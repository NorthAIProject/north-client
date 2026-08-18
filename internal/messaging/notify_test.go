package messaging_test

import (
	"context"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/messaging"
)

type stubTransport struct {
	platform string
	sent     []messaging.OutboundMessage
	to       []string
}

func (s *stubTransport) Platform() string { return s.platform }

func (s *stubTransport) Send(_ context.Context, externalID string, msg messaging.OutboundMessage) error {
	s.to = append(s.to, externalID)
	s.sent = append(s.sent, msg)
	return nil
}

func TestNotifySendsToALinkedChat(t *testing.T) {
	h := newHarness(t, &fake.Client{Responses: []fake.Response{{Text: "ok"}}}, harnessOptions{})
	h.link(t, "884422")

	bus := &stubTransport{platform: messaging.PlatformTelegram}
	svc := messaging.NewService(messaging.Options{
		Links:     messaging.NewRepository(h.pool),
		Transport: bus,
	})

	if err := svc.Notify(context.Background(), h.user.ID, "Sleep more. Lift today."); err != nil {
		t.Fatal(err)
	}
	if len(bus.sent) != 1 || bus.to[0] != "884422" {
		t.Fatalf("sent %+v to %+v", bus.sent, bus.to)
	}
	if bus.sent[0].Text != "Sleep more. Lift today." {
		t.Fatalf("text = %q", bus.sent[0].Text)
	}
}

func TestNotifyIsQuietWithoutATransport(t *testing.T) {
	h := newHarness(t, &fake.Client{}, harnessOptions{})
	if err := h.messaging.Notify(context.Background(), h.user.ID, "hello"); err != nil {
		t.Fatal(err)
	}
}
