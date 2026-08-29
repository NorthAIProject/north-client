package telegram

import (
	"context"
	"encoding/json"
	"errors"
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

func TestGetMeReportsWhoTheTokenBelongsTo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/getMe") {
			t.Errorf("called %s, want getMe", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"id":42,"username":"north_test_bot","first_name":"Khepri","can_read_all_group_messages":false}}`))
	}))
	defer srv.Close()

	client := NewClient("test-token")
	client.baseURL = srv.URL

	info, err := client.GetMe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Username != "north_test_bot" || info.ID != 42 || info.FirstName != "Khepri" {
		t.Errorf("got %+v", info)
	}
}

func TestGetMeSurfacesAnUnauthorizedToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"ok":false,"description":"Unauthorized"}`))
	}))
	defer srv.Close()

	client := NewClient("bad-token")
	client.baseURL = srv.URL

	_, err := client.GetMe(context.Background())
	if err == nil {
		t.Fatal("a rejected token returned no error")
	}
	// Telegram's own description is what makes the failure diagnosable, and it
	// carries nothing secret.
	if !strings.Contains(err.Error(), "Unauthorized") {
		t.Errorf("error does not carry the reason: %v", err)
	}
	// The token must never appear in an error that gets logged.
	if strings.Contains(err.Error(), "bad-token") {
		t.Error("the bot token leaked into an error message")
	}
}

func TestGetWebhookInfoReportsWhereUpdatesGo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":{"url":"https://north.example/webhooks/telegram","pending_update_count":3,"last_error_message":"Wrong response from the webhook: 502 Bad Gateway","last_error_date":1787000000}}`))
	}))
	defer srv.Close()

	client := NewClient("test-token")
	client.baseURL = srv.URL

	hook, err := client.GetWebhookInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !hook.Set() {
		t.Error("Set() is false for a registered webhook")
	}
	if hook.PendingUpdateCount != 3 {
		t.Errorf("PendingUpdateCount = %d, want 3", hook.PendingUpdateCount)
	}
	if hook.LastErrorMessage == "" {
		t.Error("the last delivery error was dropped; it is the fastest answer to 'why is nothing arriving'")
	}
}

func TestGetWebhookInfoOnAPollingBot(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"result":{"url":"","pending_update_count":0}}`))
	}))
	defer srv.Close()

	client := NewClient("test-token")
	client.baseURL = srv.URL

	hook, err := client.GetWebhookInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if hook.Set() {
		t.Error("Set() is true for an empty url")
	}
}

// The one polling failure that never resolves by waiting. It has to be
// distinguishable, or the poller retries a configuration mistake forever and
// logs it as if it were a network blip.
func TestGetUpdatesIdentifiesAWebhookConflict(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"ok":false,"description":"Conflict: can't use getUpdates method while webhook is active"}`))
	}))
	defer srv.Close()

	client := NewClient("test-token")
	client.baseURL = srv.URL

	_, err := client.getUpdates(context.Background(), 0, 1)
	if !errors.Is(err, ErrWebhookActive) {
		t.Fatalf("got %v, want it to wrap ErrWebhookActive", err)
	}
	// Telegram's own wording is kept, because it is what someone will search for.
	if !strings.Contains(err.Error(), "webhook is active") {
		t.Errorf("the original description was dropped: %v", err)
	}
}

// Any other refusal must stay an ordinary error, or the poller would stop
// retrying things that are worth retrying.
func TestGetUpdatesLeavesOtherFailuresAlone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"description":"Bad Gateway"}`))
	}))
	defer srv.Close()

	client := NewClient("test-token")
	client.baseURL = srv.URL

	_, err := client.getUpdates(context.Background(), 0, 1)
	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, ErrWebhookActive) {
		t.Error("an unrelated failure was classified as a webhook conflict")
	}
}

// A bot token is the entire authentication for a bot, so a token in a log file
// is the bot. Telegram puts it in the request path, and net/http prints the
// whole URL in every transport error — so the leak is on the path that only
// happens when Telegram cannot be reached at all, which is exactly the path no
// httptest-based test exercises.
//
// This was a real leak, found by running the app rather than the suite:
//
//	msg="could not publish the telegram command menu"
//	error="... Post \"https://api.telegram.org/bot<TOKEN>/setMyCommands\": context canceled"
func TestATransportFailureDoesNotLeakTheToken(t *testing.T) {
	const token = "8111737206:AAsupersecretvaluethatmustnotappear"

	// A server that is closed before use, so the request cannot connect and the
	// failure happens in the transport rather than in Telegram's envelope.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closedURL := srv.URL
	srv.Close()

	client := NewClient(token)
	client.baseURL = closedURL

	_, err := client.GetMe(context.Background())
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("the bot token leaked into an error:\n%v", err)
	}
	if strings.Contains(err.Error(), "AAsupersecret") {
		t.Fatalf("part of the bot token leaked into an error:\n%v", err)
	}
	// Still has to be diagnosable: naming the method is what makes the log line
	// worth having at all.
	if !strings.Contains(err.Error(), "getMe") {
		t.Errorf("error does not say which call failed: %v", err)
	}
}

// Cancellation must stay recognisable after the URL is stripped, or a shutdown
// reads as an outage and the poller logs it as a failure on the way down.
//
// Unwrapping *url.Error rather than scrubbing its text is what preserves this:
// the string form would still say "context canceled", but errors.Is would not.
func TestACancelledRequestStaysRecognisable(t *testing.T) {
	const token = "8111737206:AAtokenthatmustnotappear"

	api := newBotAPI(t)
	client := api.client()
	client.token = token

	// Cancelled before the call, so the transport fails immediately and
	// deterministically. A handler that blocks until the client disconnects
	// deadlocks against httptest.Server.Close, which waits for it.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.GetMe(ctx)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancellation is no longer recognisable: %v", err)
	}
	if strings.Contains(err.Error(), token) {
		t.Errorf("the token leaked: %v", err)
	}
}
