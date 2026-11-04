package coach_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/coach"
)

func ids(messages []coach.Message) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(messages))
	for _, m := range messages {
		out = append(out, m.ID)
	}
	return out
}

func sameIDs(a, b []uuid.UUID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// The page is the newest 200 and still reads oldest to newest: iOS and the web
// both draw messages in array order and take the last one as the latest.
func TestALongThreadShowsItsNewestMessagesInReadingOrder(t *testing.T) {
	t.Parallel()

	f := newThreadFixture(t, fake.Text("unused"))
	conversationID, seeded := f.seedThread(t, 205)

	detail := f.show(t, conversationID, "")

	if !sameIDs(ids(detail.Messages), seeded[5:]) {
		t.Fatalf("showed %d messages, last %v; want the newest 200 in order, ending %v",
			len(detail.Messages), detail.Messages[len(detail.Messages)-1].ID, seeded[204])
	}
	if detail.NextBefore == nil || *detail.NextBefore != seeded[5] {
		t.Errorf("nextBefore = %v, want the oldest row of the page, %v", detail.NextBefore, seeded[5])
	}

	older := f.show(t, conversationID, "?before="+detail.NextBefore.String())
	if !sameIDs(ids(older.Messages), seeded[:5]) {
		t.Errorf("older page = %v, want %v", ids(older.Messages), seeded[:5])
	}
	if older.NextBefore != nil {
		t.Errorf("nextBefore = %v on the first page of the thread, want none", older.NextBefore)
	}
}

// A client that has never heard of the cursor sees no new field until a thread
// has more than it is shown.
func TestAThreadThatFitsHasNoCursor(t *testing.T) {
	t.Parallel()

	f := newThreadFixture(t, fake.Text("unused"))
	conversationID, seeded := f.seedThread(t, 200)

	detail := f.show(t, conversationID, "")
	if !sameIDs(ids(detail.Messages), seeded) {
		t.Fatalf("showed %d messages, want all 200", len(detail.Messages))
	}
	if detail.NextBefore != nil {
		t.Errorf("nextBefore = %v, want none", detail.NextBefore)
	}
}

func TestACursorThatDoesNotBelongFindsNothing(t *testing.T) {
	t.Parallel()

	f := newThreadFixture(t, fake.Text("unused"))
	conversationID, _ := f.seedThread(t, 2)
	otherID, other := f.seedThread(t, 2)
	if otherID == conversationID {
		t.Fatal("seeded the same thread twice")
	}

	detail := f.show(t, conversationID, "?before="+other[1].String())
	if len(detail.Messages) != 0 || detail.NextBefore != nil {
		t.Errorf("a cursor from another thread returned %d messages, nextBefore %v", len(detail.Messages), detail.NextBefore)
	}

	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/conversations/"+conversationID.String()+"?before=yesterday", nil))
	if rec.Code < 400 || rec.Code >= 500 {
		t.Errorf("a malformed cursor answered %d, want a client error", rec.Code)
	}
}

// What a thread is waiting for is about its end, whichever page is on screen. On
// a long thread it used to be read off row 200, so an owed reply was never asked
// for and the client sat waiting.
func TestALongThreadStillSaysAReplyIsOwed(t *testing.T) {
	t.Parallel()

	f := newThreadFixture(t, fake.Text("unused"))
	ctx := context.Background()
	conversationID, seeded := f.seedThread(t, 205)
	calls := []ai.ToolCall{{ID: "c1", Name: "create_check_in", Arguments: json.RawMessage(`{"mood":4}`)}}
	if _, err := f.convos.AppendToolCalls(ctx, conversationID, calls, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := f.convos.AppendToolResults(ctx, conversationID, []ai.ToolResult{{ID: "c1", Name: "create_check_in", Content: "logged"}}); err != nil {
		t.Fatal(err)
	}

	if detail := f.show(t, conversationID, ""); !detail.AwaitingResume {
		t.Error("the newest page does not say a reply is owed")
	}
	if detail := f.show(t, conversationID, "?before="+seeded[50].String()); !detail.AwaitingResume {
		t.Error("an older page does not say a reply is owed")
	}
}

// The rating attaches to whatever done names. On a thread past the first page it
// named an old reply, and finding even that read 200 full rows after every turn.
func TestDoneNamesTheNewReplyOnALongThread(t *testing.T) {
	t.Parallel()

	f := newThreadFixture(t, fake.Text("The newest answer."))
	conversationID, _ := f.seedThread(t, 205)
	f.repo.firstPageReads.Store(0)

	got := f.post(t, "/conversations/"+conversationID.String()+"/reply", `{"text":"one more"}`)

	recent, err := f.convos.Recent(context.Background(), conversationID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if recent[0].Content != "The newest answer." {
		t.Fatalf("last stored turn = %q", recent[0].Content)
	}
	if got != recent[0].ID {
		t.Errorf("done named %v, want the new reply %v", got, recent[0].ID)
	}
	if n := f.repo.firstPageReads.Load(); n != 0 {
		t.Errorf("the turn read the thread's first page %d times", n)
	}
}

// A turn that stored nothing has nothing to rate. Naming the previous answer
// would attach the rating to a reply the person is not looking at.
func TestDoneNamesNothingWhenNoReplyWasStored(t *testing.T) {
	t.Parallel()

	f := newThreadFixture(t, fake.Text(""))
	conversationID, _ := f.seedThread(t, 2)

	if got := f.post(t, "/conversations/"+conversationID.String()+"/reply", `{"text":"one more"}`); got != uuid.Nil {
		t.Errorf("done named %v, want nothing", got)
	}
}

func (f threadFixture) show(t *testing.T, conversationID uuid.UUID, query string) coach.ConversationDetail {
	t.Helper()

	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/conversations/"+conversationID.String()+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("show = %d: %s", rec.Code, rec.Body.String())
	}
	var detail coach.ConversationDetail
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return detail
}

// post sends a stream request and returns the done frame's message id, or Nil
// when the frame carries none.
func (f threadFixture) post(t *testing.T, path, body string) uuid.UUID {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s = %d: %s", path, rec.Code, rec.Body.String())
	}

	_, frame, found := strings.Cut(rec.Body.String(), "event: done\ndata: ")
	if !found {
		t.Fatalf("no done frame in %q", rec.Body.String())
	}
	data, _, _ := strings.Cut(frame, "\n")
	var done coach.DoneEvent
	if err := json.Unmarshal([]byte(data), &done); err != nil {
		t.Fatalf("decode done: %v", err)
	}
	if done.MessageID == nil {
		return uuid.Nil
	}
	return *done.MessageID
}
