package coach_test

import (
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
)

// End to end through the mounted route: a read-only tool that runs inside the
// stream is announced as a named status frame before the reply that uses it.
// This is the wiring the unit tests cannot see — that the coach's loop hands
// the calls to the handler at all.
func TestARunningToolReachesTheBrowserAsAStatusFrame(t *testing.T) {
	tools := &stubTools{
		tools:    []ai.Tool{searchTool},
		results:  map[string]string{"search_exercises": "Pull-up, Row."},
		readOnly: map[string]bool{"search_exercises": true},
	}
	client := &fake.Client{Responses: []fake.Response{
		fake.Calling(fake.ToolCall("search_exercises", `{"muscle":"back"}`)),
		{Text: "Try pull-ups."},
	}}

	h := newToolHarness(t, client, tools)
	conversationID := newConversation(t, h)
	r, cookies := streamRouter(t, h, 1000)

	body := openStream(t, r, cookies, conversationID).Body.String()

	status := strings.Index(body, "event: status\ndata: is looking up exercises\n\n")
	if status < 0 {
		t.Fatalf("no status frame for the running tool; body = %q", body)
	}
	if reply := strings.Index(body, "pull-ups"); reply < 0 || reply < status {
		t.Errorf("the reply did not follow the status frame; body = %q", body)
	}
	if strings.Contains(body, "event: failed") {
		t.Errorf("a successful turn signalled failure; body = %q", body)
	}
	if !strings.Contains(body, "event: done") {
		t.Error("the stream never sent done; the browser reconnects forever")
	}

	waitForReply(t, h, conversationID, 2*time.Second)
}

// A turn that suspends for approval announces nothing: the write has not run,
// and "is logging your check-in" over an approval card would be a lie.
func TestASuspendedWriteIsNotAnnounced(t *testing.T) {
	tools := &stubTools{
		tools:    []ai.Tool{writeTool},
		results:  map[string]string{"create_check_in": "Logged."},
		readOnly: map[string]bool{"create_check_in": false},
	}
	client := &fake.Client{Responses: []fake.Response{
		fake.Calling(fake.ToolCall("create_check_in", `{"mood":4}`)),
	}}

	h := newToolHarness(t, client, tools)
	conversationID := newConversation(t, h)
	r, cookies := streamRouter(t, h, 1000)

	body := openStream(t, r, cookies, conversationID).Body.String()
	if strings.Contains(body, "event: status") {
		t.Errorf("a write waiting for approval was announced as running; body = %q", body)
	}
}
