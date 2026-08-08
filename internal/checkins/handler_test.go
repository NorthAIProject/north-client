// This file is in-package rather than checkins_test because the auth context
// key is unexported, so a request carrying a signed-in user cannot be built
// from outside. renderForm takes the user directly, which is the branch worth
// pinning: upsert and update reach it in three lines of straight-line code.
package checkins

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
	checkinpages "github.com/NorthAIProject/north-client/web/checkins"
)

func handlerUser(t *testing.T, pool *pgxpool.Pool) users.User {
	t.Helper()
	u, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        "handler@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Test User",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u
}

func newTestHandler(pool *pgxpool.Pool) *Handler {
	goalSvc := goals.NewService(goals.NewRepository(pool))
	return NewHandler(NewService(NewRepository(pool), goalSvc), goalSvc)
}

// A rejected check-in used to re-render the whole page with saved=true, so the
// user was told "Saved for today." directly above the error they had to fix,
// and the wizard reset to the first pane. Over htmx it must now come back as
// the panel fragment alone, unsaved, opened on the field that failed.
func TestRenderFormHTMXReturnsPanelWithoutSuccessBanner(t *testing.T) {
	pool := testdb.New(t)
	user := handlerUser(t, pool)
	h := newTestHandler(pool)

	form := checkinpages.CheckInForm{
		Mood:       4,
		Energy:     2,
		Wins:       "shipped the thing",
		Challenges: "slept badly",
		Notes:      strings.Repeat("x", 1001),
		Errors:     map[string]string{"notes": "Keep notes under 1000 characters."},
	}

	r := httptest.NewRequest(http.MethodPost, "/app/check-ins", nil)
	r.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()

	h.renderForm(w, r, user, form, http.StatusUnprocessableEntity)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusUnprocessableEntity)
	}

	body := w.Body.String()
	if !strings.Contains(body, `id="`+checkinpages.PanelID+`"`) {
		t.Errorf("response is missing the swap target #%s", checkinpages.PanelID)
	}
	if strings.Contains(body, "<html") {
		t.Error("htmx request got a full page back, not the panel fragment")
	}
	if strings.Contains(body, "Saved for today.") {
		t.Error("success banner rendered on a validation failure")
	}
	if !strings.Contains(body, "Keep notes under 1000 characters.") {
		t.Error("field error not rendered inline")
	}
	// The notes pane is step 5; reopening on step 1 would make the user walk
	// forward through four panes to reach the field that failed.
	if !strings.Contains(body, "step: 5") {
		t.Error("form did not reopen on the failing step")
	}
	for _, keep := range []string{"shipped the thing", "slept badly"} {
		if !strings.Contains(body, keep) {
			t.Errorf("answer %q was dropped on re-render", keep)
		}
	}
}

// The no-JavaScript path still has to work: a plain post gets the whole page,
// and it must not claim the check-in was saved either.
func TestRenderFormPlainRequestReturnsFullPage(t *testing.T) {
	pool := testdb.New(t)
	user := handlerUser(t, pool)
	h := newTestHandler(pool)

	form := checkinpages.CheckInForm{
		Mood:   0,
		Energy: 3,
		Errors: map[string]string{"mood": "Pick a mood from 1 to 5."},
	}

	r := httptest.NewRequest(http.MethodPost, "/app/check-ins", nil)
	w := httptest.NewRecorder()

	h.renderForm(w, r, user, form, http.StatusUnprocessableEntity)

	body := w.Body.String()
	if !strings.Contains(body, "<html") {
		t.Error("plain request should get the full page")
	}
	if strings.Contains(body, "Saved for today.") {
		t.Error("success banner rendered on a validation failure")
	}
	if !strings.Contains(body, "Pick a mood from 1 to 5.") {
		t.Error("field error not rendered inline")
	}
}
