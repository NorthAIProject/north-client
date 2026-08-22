package coach_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/quota"
)

// streamRouter mounts the chat routes the way main.go does — real session,
// real middleware — with a coach budget the caller chooses.
func streamRouter(t *testing.T, h harness, perHour int) (http.Handler, []*http.Cookie) {
	t.Helper()

	quotas := quota.NewService(
		quota.NewRepository(h.pool),
		quota.NewLimits(map[quota.Action]quota.Limit{quota.CoachMessage: {PerWindow: perHour, Window: time.Hour}}, nil),
		func(ctx context.Context) (quota.Identity, bool) {
			u, ok := auth.UserFrom(ctx)
			return quota.Identity{UserID: u.ID, Tier: string(u.Tier)}, ok
		},
	)

	sessions := auth.NewSessionStore(h.pool, time.Hour)
	mw := auth.NewMiddleware(sessions, false)

	r := chi.NewRouter()
	r.Use(mw.LoadUser)
	r.Group(func(r chi.Router) {
		r.Use(mw.RequireAuth)
		coach.NewHandler(h.coach, quotas).Routes(r)
	})

	token, expires, err := sessions.Create(context.Background(), h.user.ID, auth.Metadata{})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	return r, []*http.Cookie{{Name: auth.SessionCookieName, Value: token, Path: "/", Expires: expires}}
}

func openStream(t *testing.T, r http.Handler, cookies []*http.Cookie, conversationID uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/chat/"+conversationID.String()+"/stream?m=hello", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// The refusal has to arrive as an SSE frame. By the time the budget is checked
// the response is already committed as an event stream, so a 429 status would
// be a header the browser has long since parsed as a successful stream — the
// page would sit on an empty bubble forever.
func TestARefusedCoachMessageArrivesAsAStreamError(t *testing.T) {
	client := &fake.Client{Responses: []fake.Response{
		{Text: "first reply"},
		{Text: "second reply"},
	}}
	h := newHarness(t, client)

	conversation, err := h.convos.Start(context.Background(), h.user.ID)
	if err != nil {
		t.Fatalf("start conversation: %v", err)
	}

	r, cookies := streamRouter(t, h, 1)

	// The first turn spends the whole budget.
	if rec := openStream(t, r, cookies, conversation.ID); rec.Code != http.StatusOK {
		t.Fatalf("first stream status = %d, want 200", rec.Code)
	}
	callsAfterFirst := len(client.Calls())

	rec := openStream(t, r, cookies, conversation.ID)
	body := rec.Body.String()

	if !strings.Contains(body, "event: error") {
		t.Errorf("the refused stream sent no error frame; body = %q", body)
	}
	if !strings.Contains(body, "event: done") {
		t.Error("the refused stream never sent done; the browser reconnects forever")
	}
	if !strings.Contains(body, "limit") {
		t.Errorf("the error frame does not say a limit was hit; body = %q", body)
	}
	if got := len(client.Calls()); got != callsAfterFirst {
		t.Errorf("the model was called %d times after the refusal, want %d — the refusal cost money anyway",
			got-callsAfterFirst, 0)
	}
}
