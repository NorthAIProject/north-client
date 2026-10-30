package checkinreply_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/checkinreply"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
)

func TestReplyShape(t *testing.T) {
	t.Parallel()
	apitest.AssertGolden(t, "reply.golden.json", checkinreply.ReplyView{
		Kind: checkinreply.KindPattern,
		Text: "You sleep 4 or 5 and your energy follows. Worth guarding tonight's bedtime.",
	})
}

func serveAPI(t *testing.T, f fixture, method, path string, register func(*echo.Group)) *httptest.ResponseRecorder {
	t.Helper()
	e := echox.New(echox.Config{})
	register(e.Group(""))
	req := httptest.NewRequest(method, path, nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), f.user))
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	return w
}

// The app asks once after a save: a note is a 200, anything else a 204 it
// shows nothing for.
func TestAPIAnswersANoteOrNothing(t *testing.T) {
	f := setup(t)
	api := checkinreply.NewAPI(f.svc)

	if w := serveAPI(t, f, http.MethodPost, "/check-ins/today/reply", api.Register); w.Code != http.StatusNoContent {
		t.Fatalf("before a check-in: %d, want 204", w.Code)
	}

	f.checkIn(t, checkins.Input{Mood: 3, Energy: 2})
	w := serveAPI(t, f, http.MethodPost, "/check-ins/today/reply", api.Register)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"kind":"daily"`) {
		t.Fatalf("after a check-in: %d %s", w.Code, w.Body)
	}

	f.checkIn(t, checkins.Input{Mood: 1, Energy: 1})
	f.writer.err = errors.New("down")
	if w := serveAPI(t, f, http.MethodPost, "/check-ins/today/reply", api.Register); w.Code != http.StatusNoContent {
		t.Fatalf("model down: %d, want 204", w.Code)
	}
}
