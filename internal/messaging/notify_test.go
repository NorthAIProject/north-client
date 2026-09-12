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

func TestNotifyMessageCarriesAPhoto(t *testing.T) {
	// Notify takes a string, which is all the briefing ever needed. The
	// digest has a picture, and a fan-out that silently dropped it would be
	// very hard to notice.
	h := newHarness(t, &fake.Client{Responses: []fake.Response{{Text: "ok"}}}, harnessOptions{})
	h.link(t, "884422")

	bus := &stubTransport{platform: messaging.PlatformTelegram}
	svc := messaging.NewService(messaging.Options{
		Links:     messaging.NewRepository(h.pool),
		Transport: bus,
	})

	png := []byte("\x89PNG fake")
	err := svc.NotifyMessage(context.Background(), h.user.ID, messaging.OutboundMessage{
		Text:         "Body — Strong (88)",
		Photo:        png,
		PhotoCaption: "Last 7 days",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(bus.sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(bus.sent))
	}
	if string(bus.sent[0].Photo) != string(png) {
		t.Error("the photo did not survive the fan-out")
	}
	if bus.sent[0].PhotoCaption != "Last 7 days" {
		t.Errorf("caption = %q", bus.sent[0].PhotoCaption)
	}
}

func TestNotifyMessageWithAPhotoAndNoTextStillSends(t *testing.T) {
	// Notify treats blank text as nothing to say. A card with its story in
	// the caption is something to say.
	h := newHarness(t, &fake.Client{Responses: []fake.Response{{Text: "ok"}}}, harnessOptions{})
	h.link(t, "884422")

	bus := &stubTransport{platform: messaging.PlatformTelegram}
	svc := messaging.NewService(messaging.Options{
		Links:     messaging.NewRepository(h.pool),
		Transport: bus,
	})

	err := svc.NotifyMessage(context.Background(), h.user.ID, messaging.OutboundMessage{
		Photo:        []byte("png"),
		PhotoCaption: "Last 7 days",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(bus.sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(bus.sent))
	}
}

func TestNotifyMessageWithNothingToSaySendsNothing(t *testing.T) {
	h := newHarness(t, &fake.Client{Responses: []fake.Response{{Text: "ok"}}}, harnessOptions{})
	h.link(t, "884422")

	bus := &stubTransport{platform: messaging.PlatformTelegram}
	svc := messaging.NewService(messaging.Options{
		Links:     messaging.NewRepository(h.pool),
		Transport: bus,
	})

	if err := svc.NotifyMessage(context.Background(), h.user.ID, messaging.OutboundMessage{}); err != nil {
		t.Fatal(err)
	}
	if len(bus.sent) != 0 {
		t.Errorf("sent %d messages for an empty notification", len(bus.sent))
	}
}
