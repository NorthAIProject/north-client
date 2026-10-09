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

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/checkins/checkin"
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

// A save over htmx swaps only the form's panel; the history, KPIs and charts
// refresh themselves on the event the response announces. It must be aimed at
// body, because the form that sent the request has been swapped out by then.
func TestRenderSavedAnnouncesTheSave(t *testing.T) {
	pool := testdb.New(t)
	user := handlerUser(t, pool)
	h := newTestHandler(pool)

	saved, err := h.svc.UpsertToday(context.Background(), user, Input{Mood: 4, Energy: 3})
	if err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest(http.MethodPost, "/app/check-ins", nil)
	r.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	h.renderSaved(w, r, user, saved)

	if got := w.Header().Get("HX-Trigger"); got != `{"checkin-saved":{"target":"body"}}` {
		t.Fatalf("HX-Trigger = %q", got)
	}
	if !strings.Contains(w.Body.String(), `id="`+checkinpages.PanelID+`"`) {
		t.Error("saved response lost the panel swap target")
	}

	// The no-JavaScript path redirects; nothing listens there.
	plain := httptest.NewRecorder()
	h.renderSaved(plain, httptest.NewRequest(http.MethodPost, "/app/check-ins", nil), user, saved)
	if plain.Code != http.StatusSeeOther || plain.Header().Get("HX-Trigger") != "" {
		t.Fatalf("plain post: status %d, HX-Trigger %q", plain.Code, plain.Header().Get("HX-Trigger"))
	}
}

// The live region polls every 20 seconds with the version it shows. Unchanged
// must be a 204, which htmx does not swap; changed must redraw it.
func TestLiveAnswers204UntilSomethingChanges(t *testing.T) {
	pool := testdb.New(t)
	user := handlerUser(t, pool)
	h := newTestHandler(pool)
	ctx := auth.ContextWithUser(context.Background(), user)

	get := func(version string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, checkinpages.LiveURL(version), nil).WithContext(ctx)
		r.Header.Set("HX-Request", "true")
		w := httptest.NewRecorder()
		h.live(w, r)
		return w
	}

	first := get("")
	if first.Code != http.StatusOK {
		t.Fatalf("stale version: status %d, want 200", first.Code)
	}
	version, err := h.svc.Version(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first.Body.String(), `id="`+checkinpages.LiveID+`"`) ||
		!strings.Contains(first.Body.String(), "checkin-saved from:body, every 20s") {
		t.Fatalf("live region missing its id or trigger:\n%s", first.Body.String())
	}

	if w := get(version); w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Fatalf("unchanged version: status %d body %q, want an empty 204", w.Code, w.Body.String())
	}

	if _, err = h.svc.UpsertToday(ctx, user, Input{Mood: 5, Energy: 4, Tags: []string{"travel"}, Source: checkin.SourceWeb}); err != nil {
		t.Fatal(err)
	}
	changed := get(version)
	if changed.Code != http.StatusOK {
		t.Fatalf("after a save: status %d, want 200", changed.Code)
	}
	if !strings.Contains(changed.Body.String(), "travel") {
		t.Error("redrawn region does not show the new check-in's tag")
	}
}

// Every field the form sends reaches the service, and blank optional scales
// mean "not given".
func TestFormCarriesTheExtras(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/app/check-ins",
		strings.NewReader("mood=4&energy=3&stress=2&sleep_quality=&tags=Travel,+sick"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := r.ParseForm(); err != nil {
		t.Fatal(err)
	}
	in := inputFrom(formFrom(r))
	if in.Stress == nil || *in.Stress != 2 {
		t.Errorf("stress = %v, want 2", in.Stress)
	}
	if in.SleepQuality != nil {
		t.Errorf("blank sleep quality should be nil, got %v", *in.SleepQuality)
	}
	if in.Source != checkin.SourceWeb {
		t.Errorf("source = %q, want web", in.Source)
	}
	clean, err := Validate(in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(clean.Tags, "|") != "travel|sick" {
		t.Errorf("tags = %q", clean.Tags)
	}
}
