package coach_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// searcher is a provider with a web search of its own: it answers as the fake
// does and then names the pages it cited, as the Anthropic client does.
type searcher struct {
	*fake.Client
	cited []ai.Source

	mu      sync.Mutex
	allowed []bool
}

func (s *searcher) Chat(ctx context.Context, req ai.Request) (<-chan ai.StreamChunk, error) {
	s.mu.Lock()
	s.allowed = append(s.allowed, req.AllowWebSearch)
	s.mu.Unlock()

	inner, err := s.Client.Chat(ctx, req)
	if err != nil {
		return nil, err
	}
	out := make(chan ai.StreamChunk)
	go func() {
		defer close(out)
		var last []ai.StreamChunk
		for chunk := range inner {
			// Sources come after the text and before usage.
			if chunk.Usage != nil || chunk.Err != nil {
				last = append(last, chunk)
				continue
			}
			out <- chunk
		}
		out <- ai.StreamChunk{Sources: s.cited}
		for _, chunk := range last {
			out <- chunk
		}
	}()
	return out, nil
}

// A reply that used the web says where from, in the reply's own text, so the
// web app, a phone build from before this existed and Telegram all show it.
// Titles and addresses are the web's, and are not trusted.
func TestAReplyThatUsedTheWebListsItsSources(t *testing.T) {
	t.Parallel()

	answer := fake.Text("Loading is optional; 3 g a day gets there in a month.")
	client := &searcher{Client: answer, cited: []ai.Source{
		{Title: "Creatine [2026] review](https://evil.test)", URL: "https://example.org/creatine_(review)"},
		{Title: "Creatine [2026] review", URL: "https://example.org/creatine_(review)"},
		{Title: "", URL: "https://www.example.com/dosing?a=1"},
		{Title: "Click me", URL: "javascript:alert(1)"},
	}}
	h := newExternalHarnessOver(t, client, answer, nil)
	conversationID := newConversation(t, h)

	ctx := i18n.WithLocale(context.Background(), "es")
	stream, err := h.coach.SendMessage(ctx, h.user, conversationID, "do I need to load creatine?")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	streamed, err := drain(stream)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}

	want := "Loading is optional; 3 g a day gets there in a month.\n\n" +
		"**Fuentes**\n" +
		"- [Creatine (2026) review)(https://evil.test)](https://example.org/creatine_%28review%29)\n" +
		"- [example.com](https://www.example.com/dosing?a=1)"
	if streamed != want {
		t.Errorf("streamed:\n%s\nwant:\n%s", streamed, want)
	}
	if stored := waitForReply(t, h, conversationID, 5*time.Second); stored.Content != want {
		t.Errorf("stored:\n%s\nwant:\n%s", stored.Content, want)
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.allowed) != 1 || !client.allowed[0] {
		t.Errorf("AllowWebSearch per request = %v, want the coach's reply to allow it", client.allowed)
	}
}

func TestAReplyThatCitedNothingHasNoSourceList(t *testing.T) {
	t.Parallel()

	answer := fake.Text("Sit back and down.")
	h := newExternalHarnessOver(t, &searcher{Client: answer}, answer, nil)
	conversationID := newConversation(t, h)

	stream, err := h.coach.SendMessage(context.Background(), h.user, conversationID, "how do I squat?")
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	streamed, err := drain(stream)
	if err != nil {
		t.Fatalf("drain: %v", err)
	}
	if streamed != "Sit back and down." || strings.Contains(streamed, "Sources") {
		t.Errorf("streamed = %q", streamed)
	}
}
