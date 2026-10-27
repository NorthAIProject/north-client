package checkinreply_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/checkinreply"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestReplyShape(t *testing.T) {
	t.Parallel()
	apitest.AssertGolden(t, "reply.golden.json", checkinreply.ReplyView{
		Kind: checkinreply.KindPattern,
		Text: "You sleep 4 or 5 and your energy follows. Worth guarding tonight's bedtime.",
	})
}

func serve(t *testing.T, f fixture, method, path string, routes func(chi.Router)) *httptest.ResponseRecorder {
	t.Helper()
	r := chi.NewRouter()
	routes(r)
	req := httptest.NewRequest(method, path, nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), f.user))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// The app asks once after a save: a note is a 200, anything else a 204 it
// shows nothing for.
func TestAPIAnswersANoteOrNothing(t *testing.T) {
	f := setup(t)
	api := checkinreply.NewAPI(f.svc)

	if w := serve(t, f, http.MethodPost, "/check-ins/today/reply", api.Routes); w.Code != http.StatusNoContent {
		t.Fatalf("before a check-in: %d, want 204", w.Code)
	}

	f.checkIn(t, checkins.Input{Mood: 3, Energy: 2})
	w := serve(t, f, http.MethodPost, "/check-ins/today/reply", api.Routes)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"kind":"daily"`) {
		t.Fatalf("after a check-in: %d %s", w.Code, w.Body)
	}

	f.checkIn(t, checkins.Input{Mood: 1, Energy: 1})
	f.writer.err = errors.New("down")
	if w := serve(t, f, http.MethodPost, "/check-ins/today/reply", api.Routes); w.Code != http.StatusNoContent {
		t.Fatalf("model down: %d, want 204", w.Code)
	}
}

// htmx does not swap a 204, so the page route answers 200 with an empty body
// when there is no note, or the placeholder would stay forever.
func TestPageSlotIsEmptyRatherThan204(t *testing.T) {
	f := setup(t)
	h := checkinreply.NewHandler(f.svc)

	w := serve(t, f, http.MethodGet, "/check-ins/today/reply", h.Routes)
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != "" {
		t.Fatalf("no check-in: %d %q, want 200 and empty", w.Code, w.Body)
	}

	f.checkIn(t, checkins.Input{Mood: 3, Energy: 2})
	w = serve(t, f, http.MethodGet, "/check-ins/today/reply", h.Routes)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), f.writer.text) {
		t.Fatalf("with a note: %d %s", w.Code, w.Body)
	}
}
