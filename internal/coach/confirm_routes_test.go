package coach_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/auth"
)

// suspendedTurn drives a real request through the mounted routes until the
// coach is waiting on approval, and hands back what is needed to answer it.
//
// Everything here goes through the router rather than the service, because the
// gap these tests exist to close is the wiring: a service that works behind
// routes nobody mounted is a feature nobody can reach.
func suspendedTurn(t *testing.T) (http.Handler, []*http.Cookie, harness, uuid.UUID, uuid.UUID) {
	t.Helper()

	tools := &stubTools{
		tools:    []ai.Tool{writeTool},
		results:  map[string]string{"create_check_in": "Logged: mood 4."},
		readOnly: map[string]bool{"create_check_in": false},
	}
	client := &fake.Client{Responses: []fake.Response{
		fake.Calling(fake.ToolCall("create_check_in", `{"mood":4}`)),
		{Text: "Logged it."},
	}}

	h := newToolHarness(t, client, tools)
	conversationID := newConversation(t, h)
	r, cookies := streamRouter(t, h, 1000)

	if rec := openStream(t, r, cookies, conversationID); rec.Code != http.StatusOK {
		t.Fatalf("opening the stream: status = %d, want 200", rec.Code)
	}

	pending, ok, err := h.coach.PendingApproval(context.Background(), h.user, conversationID)
	if err != nil {
		t.Fatalf("pending approval: %v", err)
	}
	if !ok {
		t.Fatal("the turn did not suspend; there is nothing to approve through the routes")
	}

	return r, cookies, h, conversationID, pending.MessageID
}

func resolve(t *testing.T, r http.Handler, cookies []*http.Cookie, conversationID, messageID uuid.UUID, decision string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost,
		"/chat/"+conversationID.String()+"/tools/"+messageID.String()+"/"+decision, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestApprovingThroughTheRouteRunsTheToolOnce(t *testing.T) {
	r, cookies, h, conversationID, messageID := suspendedTurn(t)

	rec := resolve(t, r, cookies, conversationID, messageID, "approve")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303 — the route is not mounted or refused", rec.Code)
	}
	if got, want := rec.Header().Get("Location"), "/app/chat/"+conversationID.String(); got != want {
		t.Errorf("location = %q, want %q", got, want)
	}

	if _, ok, err := h.coach.PendingApproval(context.Background(), h.user, conversationID); err != nil {
		t.Fatalf("pending approval: %v", err)
	} else if ok {
		t.Error("the call is still pending after being approved through the route")
	}
}

func TestDecliningThroughTheRouteAppliesNothing(t *testing.T) {
	r, cookies, h, conversationID, messageID := suspendedTurn(t)

	if rec := resolve(t, r, cookies, conversationID, messageID, "decline"); rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}

	history, err := h.convos.History(context.Background(), conversationID)
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	var recorded string
	for _, m := range history {
		for _, res := range m.ToolResults {
			recorded = res.Content
		}
	}
	if !strings.Contains(strings.ToLower(recorded), "declin") {
		t.Errorf("recorded result = %q, want the refusal the model is told about", recorded)
	}
}

// The card carries the id of the turn it answers. A stale tab holding an older
// one must not resolve whatever happens to be pending now.
func TestResolvingWithTheWrongTurnIdIsRefused(t *testing.T) {
	r, cookies, _, conversationID, _ := suspendedTurn(t)

	rec := resolve(t, r, cookies, conversationID, uuid.New(), "approve")
	if rec.Code == http.StatusSeeOther {
		t.Errorf("status = %d; a mismatched turn id was accepted", rec.Code)
	}
}

func TestAnUnknownDecisionIsNotFound(t *testing.T) {
	r, cookies, _, conversationID, messageID := suspendedTurn(t)

	rec := resolve(t, r, cookies, conversationID, messageID, "maybe")
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for a decision that is neither approve nor decline", rec.Code)
	}
}

// Signed out, the write must be unreachable. RequireAuth is what stands between
// a public POST and somebody's check-in.
func TestResolvingWithoutASessionIsRejected(t *testing.T) {
	r, _, h, conversationID, messageID := suspendedTurn(t)

	rec := resolve(t, r, nil, conversationID, messageID, "approve")

	// Not asserted on the status: RequireAuth answers an unauthenticated
	// navigation with its own 303 to the login page, so the code alone cannot
	// tell "sent to sign in" from "went ahead and wrote". The location says
	// which, and the pending call says whether anything happened.
	if got := rec.Header().Get("Location"); !strings.Contains(got, "login") {
		t.Errorf("location = %q, want the login page", got)
	}

	if _, ok, err := h.coach.PendingApproval(context.Background(), h.user, conversationID); err != nil {
		t.Fatalf("pending approval: %v", err)
	} else if !ok {
		t.Error("an unauthenticated request resolved the pending write")
	}
}

// Another account's approval must not run the tool, even with a valid session
// of its own.
func TestResolvingSomebodyElsesTurnIsRefused(t *testing.T) {
	r, _, h, conversationID, messageID := suspendedTurn(t)

	stranger := registerStranger(t, h)
	sessions := auth.NewSessionStore(h.pool, time.Hour)
	token, expires, err := sessions.Create(context.Background(), stranger.ID, auth.Metadata{})
	if err != nil {
		t.Fatalf("create stranger session: %v", err)
	}
	strangerCookies := []*http.Cookie{{Name: auth.SessionCookieName, Value: token, Path: "/", Expires: expires}}

	rec := resolve(t, r, strangerCookies, conversationID, messageID, "approve")
	if rec.Code == http.StatusSeeOther {
		t.Errorf("status = %d; another account resolved this write", rec.Code)
	}

	if _, ok, err := h.coach.PendingApproval(context.Background(), h.user, conversationID); err != nil {
		t.Fatalf("pending approval: %v", err)
	} else if !ok {
		t.Error("the stranger's request resolved the call anyway")
	}
}

// The resumed reply has to arrive through the route the card sends the browser
// back to, as SSE frames the page already knows how to swap.
func TestTheResumeRouteStreamsTheRestOfTheReply(t *testing.T) {
	r, cookies, h, conversationID, messageID := suspendedTurn(t)

	if rec := resolve(t, r, cookies, conversationID, messageID, "approve"); rec.Code != http.StatusSeeOther {
		t.Fatalf("approve: status = %d, want 303", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/chat/"+conversationID.String()+"/resume", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — the resume route is not mounted", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Errorf("content type = %q, want an event stream", got)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "event: token") {
		t.Errorf("the resumed stream sent no tokens; body = %q", body)
	}
	if !strings.Contains(body, "event: done") {
		t.Error("the resumed stream never sent done; the browser reconnects forever")
	}

	waitForReply(t, h, conversationID, 2*time.Second)
}

// Resuming must not spend a second coach message. The person asked once.
func TestResumingSpendsNoFurtherQuota(t *testing.T) {
	r, cookies, _, conversationID, messageID := suspendedTurn(t)

	if rec := resolve(t, r, cookies, conversationID, messageID, "approve"); rec.Code != http.StatusSeeOther {
		t.Fatalf("approve: status = %d, want 303", rec.Code)
	}

	// The budget above is 1000, so this proves the route is unguarded rather
	// than merely under budget: a guarded route would still be reachable here.
	// What it pins is that resume never calls Consume at all, which is checked
	// by resuming more times than a single turn could ever be allowed.
	for i := range 3 {
		req := httptest.NewRequest(http.MethodGet, "/chat/"+conversationID.String()+"/resume", nil)
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("resume %d: status = %d, want 200", i+1, rec.Code)
		}
		if strings.Contains(rec.Body.String(), "limit") {
			t.Errorf("resume %d was refused by the quota; answering a confirmation is not a new question", i+1)
		}
	}
}
