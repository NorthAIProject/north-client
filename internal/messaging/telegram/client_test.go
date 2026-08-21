package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/messaging"
)

// botAPI records what would have been sent to Telegram.
type botAPI struct {
	mu    sync.Mutex
	calls []botCall

	srv *httptest.Server
}

type botCall struct {
	method string
	body   map[string]any
}

func newBotAPI(t *testing.T) *botAPI {
	t.Helper()

	api := &botAPI{}
	api.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Path is /bot<token>/<method>; the token must never be asserted on.
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")

		api.mu.Lock()
		api.calls = append(api.calls, botCall{method: parts[len(parts)-1], body: decodeBody(r)})
		api.mu.Unlock()

		_, _ = w.Write([]byte(`{"ok":true,"result":{}}`))
	}))
	t.Cleanup(api.srv.Close)
	return api
}

func (a *botAPI) client() *Client {
	c := NewClient("test-token")
	c.baseURL = a.srv.URL
	return c
}

func TestFileDownloadsThePhoto(t *testing.T) {
	jpeg := []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getFile"):
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_path":"photos/file_0.jpg"}}`))
		case strings.Contains(r.URL.Path, "/file/bot"):
			_, _ = w.Write(jpeg)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	c := NewClient("test-token")
	c.baseURL = srv.URL
	c.http = srv.Client()

	data, mime, err := c.File(context.Background(), "AgAC")
	if err != nil {
		t.Fatal(err)
	}
	if mime != "image/jpeg" {
		t.Fatalf("mime = %q", mime)
	}
	if string(data) != string(jpeg) {
		t.Fatalf("bytes = %d", len(data))
	}
}

// decodeBody reads one Bot API request body.
func decodeBody(r *http.Request) map[string]any {
	raw, _ := io.ReadAll(r.Body)

	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	return body
}

func (a *botAPI) sends() []botCall { return a.method("sendMessage") }

func (a *botAPI) method(name string) []botCall {
	a.mu.Lock()
	defer a.mu.Unlock()

	var out []botCall
	for _, call := range a.calls {
		if call.method == name {
			out = append(out, call)
		}
	}
	return out
}

// waitFor polls until a condition holds. The bridge answers on a detached
// goroutine, so there is no channel to wait on from out here.
func waitFor(t *testing.T, done func() bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if done() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for the bot to act")
}

func TestSendDeliversTheText(t *testing.T) {
	api := newBotAPI(t)

	err := api.client().Send(context.Background(), "884422", messaging.OutboundMessage{Text: "two workouts this week"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	sends := api.sends()
	if len(sends) != 1 {
		t.Fatalf("expected one sendMessage, got %d", len(sends))
	}
	if sends[0].body["text"] != "two workouts this week" {
		t.Fatalf("text came through as %v", sends[0].body["text"])
	}
	// Sent as a number, because that is what the Bot API documents.
	if _, ok := sends[0].body["chat_id"].(float64); !ok {
		t.Fatalf("chat_id should be numeric, got %T", sends[0].body["chat_id"])
	}
	if _, ok := sends[0].body["reply_markup"]; ok {
		t.Fatal("an ordinary reply should carry no keyboard")
	}
}

func TestOptionsBecomeAnInlineKeyboard(t *testing.T) {
	api := newBotAPI(t)

	err := api.client().Send(context.Background(), "884422", messaging.OutboundMessage{
		Text: "confirm?",
		Options: []messaging.Option{
			{Label: "Yes, do it", Value: messaging.AnswerApprove},
			{Label: "No", Value: messaging.AnswerDecline},
		},
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	markup, ok := api.sends()[0].body["reply_markup"].(map[string]any)
	if !ok {
		t.Fatal("no keyboard on a message with options")
	}
	rows, ok := markup["inline_keyboard"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("expected one row of buttons, got %v", markup["inline_keyboard"])
	}
	buttons, ok := rows[0].([]any)
	if !ok || len(buttons) != 2 {
		t.Fatalf("expected two buttons, got %v", rows[0])
	}
	first := buttons[0].(map[string]any)
	if first["text"] != "Yes, do it" || first["callback_data"] != messaging.AnswerApprove {
		t.Fatalf("button is wrong: %v", first)
	}
}

// A redelivery answers with nothing, and nothing must actually be sent.
func TestASilentMessageSendsNothing(t *testing.T) {
	api := newBotAPI(t)

	if err := api.client().Send(context.Background(), "884422", messaging.OutboundMessage{Silent: true}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if got := api.sends(); len(got) != 0 {
		t.Fatalf("a silent message was sent anyway: %v", got)
	}
}

// A client that dies at the same instant Telegram is asked to hold the
// connection open turns every quiet poll into "getUpdates request failed".
func TestTheHTTPClientOutlastsALongPoll(t *testing.T) {
	if requestTimeout <= time.Duration(pollTimeoutSeconds)*time.Second {
		t.Fatalf("requestTimeout %s must be longer than pollTimeoutSeconds %d",
			requestTimeout, pollTimeoutSeconds)
	}
}

// Telegram rejects an over-long message rather than truncating it, so Khepri
// splits — and the buttons ride on the last part, where somebody has finished
// reading before being asked to decide.
func TestALongReplyIsSplitWithButtonsOnTheLastPart(t *testing.T) {
	api := newBotAPI(t)

	long := strings.Repeat("a sentence that goes on. ", 400) // well over 4096 runes
	err := api.client().Send(context.Background(), "884422", messaging.OutboundMessage{
		Text:    long,
		Options: []messaging.Option{{Label: "Yes", Value: messaging.AnswerApprove}},
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	sends := api.sends()
	if len(sends) < 2 {
		t.Fatalf("expected the reply to be split, got %d message(s)", len(sends))
	}
	for i, call := range sends {
		text, _ := call.body["text"].(string)
		if n := len([]rune(text)); n > maxMessageRunes {
			t.Fatalf("part %d is %d runes, over the limit", i, n)
		}
		_, hasKeyboard := call.body["reply_markup"]
		if last := i == len(sends)-1; hasKeyboard != last {
			t.Fatalf("part %d: keyboard present=%v, want %v", i, hasKeyboard, last)
		}
	}
}

func TestSplitMessagePrefersPunctuationOverCuttingWords(t *testing.T) {
	text := strings.Repeat("word ", 20) + "\n\n" + strings.Repeat("more ", 20)

	parts := splitMessage(text, 60)
	if len(parts) < 2 {
		t.Fatalf("expected a split, got %d part(s)", len(parts))
	}
	for _, part := range parts {
		if len([]rune(part)) > 60 {
			t.Fatalf("part over the limit: %q", part)
		}
		if strings.HasPrefix(part, "ord") || strings.HasPrefix(part, "ore") {
			t.Fatalf("split mid-word: %q", part)
		}
	}
}

// A short message is one message, untouched.
func TestSplitMessageLeavesShortTextAlone(t *testing.T) {
	parts := splitMessage("all good", maxMessageRunes)
	if len(parts) != 1 || parts[0] != "all good" {
		t.Fatalf("got %q", parts)
	}
}

func TestAStoredChatIDThatIsNotANumberIsRefusedWithoutEchoingIt(t *testing.T) {
	api := newBotAPI(t)

	err := api.client().Send(context.Background(), "not-a-number", messaging.OutboundMessage{Text: "hi"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "not-a-number") {
		t.Fatalf("the error echoed the identifier: %v", err)
	}
}

// The token is in the URL path, which is exactly why no error may carry it.
func TestAFailedCallNeverNamesTheToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"ok":false,"description":"chat not found"}`))
	}))
	t.Cleanup(srv.Close)

	client := NewClient("super-secret-bot-token")
	client.baseURL = srv.URL

	err := client.Send(context.Background(), "884422", messaging.OutboundMessage{Text: "hi"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "super-secret-bot-token") {
		t.Fatalf("the error leaked the token: %v", err)
	}
	if !strings.Contains(err.Error(), "chat not found") {
		t.Fatalf("the error should carry Telegram's reason, got %v", err)
	}
}
