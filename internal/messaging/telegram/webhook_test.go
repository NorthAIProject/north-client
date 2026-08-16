package telegram

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/messaging"
)

// recorder stands in for the messaging service and reports when it was reached,
// so a test can tell "acknowledged before answering" from "answered quickly".
type recorder struct {
	mu       sync.Mutex
	seen     []messaging.InboundMessage
	release  chan struct{}
	answered chan struct{}
}

func newRecorder() *recorder {
	return &recorder{release: make(chan struct{}), answered: make(chan struct{}, 8)}
}

func (r *recorder) Handle(ctx context.Context, in messaging.InboundMessage) (messaging.OutboundMessage, error) {
	<-r.release

	r.mu.Lock()
	r.seen = append(r.seen, in)
	r.mu.Unlock()

	select {
	case r.answered <- struct{}{}:
	default:
	}
	return messaging.OutboundMessage{Text: "ok"}, nil
}

func (r *recorder) messages() []messaging.InboundMessage {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]messaging.InboundMessage(nil), r.seen...)
}

const testSecret = "a-secret-that-only-telegram-knows"

func newTestWebhook(t *testing.T, h Handler) *Webhook {
	t.Helper()

	// A client pointed at a server that answers everything, so a delivered
	// reply neither reaches Telegram nor fails the test.
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	t.Cleanup(stub.Close)

	client := NewClient("test-token")
	client.baseURL = stub.URL

	hook := NewWebhook(WebhookConfig{Messages: h, Client: client, Secret: testSecret})
	if hook == nil {
		t.Fatal("NewWebhook returned nil with a secret configured")
	}
	return hook
}

func post(t *testing.T, hook *Webhook, secret, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/webhooks/telegram", strings.NewReader(body))
	if secret != "" {
		req.Header.Set(secretHeader, secret)
	}
	rec := httptest.NewRecorder()
	hook.ServeHTTP(rec, req)
	return rec
}

const messageUpdate = `{"update_id":11,"message":{"message_id":1,"chat":{"id":884422},"text":"how am I doing?","date":1755300000}}`

// The secret is the only thing separating a real delivery from anyone who
// guesses the path, so a missing or wrong one must reach nothing.
func TestAnUnauthenticatedDeliveryIsRefused(t *testing.T) {
	rec := newRecorder()
	close(rec.release)
	hook := newTestWebhook(t, rec)

	for _, secret := range []string{"", "wrong-secret"} {
		resp := post(t, hook, secret, messageUpdate)
		if resp.Code != http.StatusUnauthorized {
			t.Fatalf("secret %q: got %d, want 401", secret, resp.Code)
		}
		if resp.Body.Len() != 0 {
			t.Fatalf("secret %q: the refusal should say nothing, got %q", secret, resp.Body.String())
		}
	}

	if got := rec.messages(); len(got) != 0 {
		t.Fatalf("an unauthenticated delivery reached the service: %+v", got)
	}
}

// Telegram retries anything that is not a prompt 200, and a coach turn takes
// minutes. So the update is acknowledged before it is answered — this test
// holds the service open and still expects the 200.
func TestAnUpdateIsAcknowledgedBeforeItIsAnswered(t *testing.T) {
	rec := newRecorder()
	hook := newTestWebhook(t, rec)

	resp := post(t, hook, testSecret, messageUpdate)
	if resp.Code != http.StatusOK {
		t.Fatalf("got %d, want 200 while the answer is still being generated", resp.Code)
	}
	if got := rec.messages(); len(got) != 0 {
		t.Fatal("the request waited for the answer")
	}

	close(rec.release)
	waitForAnswer(t, rec)

	got := rec.messages()
	if len(got) != 1 {
		t.Fatalf("expected one message, got %d", len(got))
	}
	if got[0].ExternalID != "884422" || got[0].Text != "how am I doing?" || got[0].UpdateID != 11 {
		t.Fatalf("decoded wrong: %+v", got[0])
	}
	if got[0].Platform != messaging.PlatformTelegram {
		t.Fatalf("platform is %q", got[0].Platform)
	}
}

// A tapped button and a typed word must arrive as the same thing, so the
// adapter needs no second path for them.
func TestATappedButtonArrivesAsItsValue(t *testing.T) {
	rec := newRecorder()
	close(rec.release)
	hook := newTestWebhook(t, rec)

	body := `{"update_id":12,"callback_query":{"id":"cb-1","data":"approve","message":{"chat":{"id":884422}}}}`
	if resp := post(t, hook, testSecret, body); resp.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", resp.Code)
	}
	waitForAnswer(t, rec)

	got := rec.messages()
	if len(got) != 1 {
		t.Fatalf("expected one message, got %d", len(got))
	}
	if got[0].Text != messaging.AnswerApprove {
		t.Fatalf("button value came through as %q", got[0].Text)
	}
	if got[0].ExternalID != "884422" {
		t.Fatalf("callback lost its chat: %q", got[0].ExternalID)
	}
}

// A sticker or a photo is not an error and not a question. Answering it would
// be worse than ignoring it.
func TestAnUpdateWithNothingToAnswerIsIgnored(t *testing.T) {
	rec := newRecorder()
	close(rec.release)
	hook := newTestWebhook(t, rec)

	for _, body := range []string{
		`{"update_id":13,"message":{"message_id":2,"chat":{"id":884422},"date":1755300000}}`,
		`{"update_id":14}`,
		`not json at all`,
	} {
		if resp := post(t, hook, testSecret, body); resp.Code != http.StatusOK {
			t.Fatalf("body %q: got %d, want 200", body, resp.Code)
		}
	}

	// Nothing should ever arrive, so a short wait is the only way to check.
	time.Sleep(50 * time.Millisecond)
	if got := rec.messages(); len(got) != 0 {
		t.Fatalf("an empty update was answered: %+v", got)
	}
}

func TestNewWebhookRefusesToRunWithoutASecret(t *testing.T) {
	if hook := NewWebhook(WebhookConfig{Messages: newRecorder(), Client: NewClient("t")}); hook != nil {
		t.Fatal("a webhook with no secret would be an open endpoint")
	}
}

func waitForAnswer(t *testing.T, rec *recorder) {
	t.Helper()
	select {
	case <-rec.answered:
	case <-time.After(2 * time.Second):
		t.Fatal("the update was never answered")
	}
}
