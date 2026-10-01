package capture_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/capture"
	"github.com/NorthAIProject/north-client/internal/quota"
	"github.com/NorthAIProject/north-client/internal/users"
)

// router mounts the capture routes the way the app does, under /app. The quota
// service is never consulted: none of these requests reach the metered parse.
func router(svc *capture.Service) http.Handler {
	quotas := quota.NewService(nil, quota.NewLimits(nil, nil), nil)
	r := chi.NewRouter()
	r.Route("/app", capture.NewHandler(svc, quotas).Routes)
	return r
}

// meteredRouter is router with a real quota service, for the routes that
// spend one: parse is guarded, and the guard needs a counter and an identity.
func meteredRouter(f *fixture) http.Handler {
	quotas := quota.NewService(
		quota.NewRepository(f.pool),
		quota.NewLimits(map[quota.Action]quota.Limit{quota.QuickCapture: {PerWindow: 1000, Window: time.Hour}}, nil),
		func(ctx context.Context) (quota.Identity, bool) {
			u, ok := auth.UserFrom(ctx)
			return quota.Identity{UserID: u.ID, Tier: string(u.Tier)}, ok
		},
	)
	r := chi.NewRouter()
	r.Route("/app", capture.NewHandler(f.svc, quotas).Routes)
	return r
}

func get(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(auth.ContextWithUser(req.Context(), users.User{DisplayName: "Ana", Timezone: "UTC"}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestPanelIsAFragmentForTheDayDialog(t *testing.T) {
	t.Parallel()

	rec := get(t, router(nil), "/app/capture/panel?return_to=/app")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()

	for _, want := range []string{
		`id="capture-text"`,
		`hx-post="/app/capture/parse"`,
		`name="return_to" value="/app"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("panel lacks %s", want)
		}
	}
	// A fragment: no layout, no page heading, and no autofocus that would raise
	// the keyboard as the dialog opens.
	for _, unwanted := range []string{"<html", "<h1", "autofocus"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("embedded panel contains %s", unwanted)
		}
	}
}

func TestPanelRendersWithoutHTMXToo(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/app/capture/panel?return_to=/app/nutrition/log&text=two+eggs", nil)
	rec := httptest.NewRecorder()
	router(nil).ServeHTTP(rec, req)

	body := rec.Body.String()
	if strings.Contains(body, "<html") {
		t.Error("the panel endpoint rendered the whole page")
	}
	if !strings.Contains(body, "two eggs") {
		t.Error("the prefilled text was dropped")
	}
	if !strings.Contains(body, `value="/app/nutrition/log"`) {
		t.Error("the food log's return_to was dropped")
	}
}

func TestPanelIgnoresAReturnToItDoesNotKnow(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"https://evil.example", "//evil.example", "/app/settings"} {
		rec := get(t, router(nil), "/app/capture/panel?return_to="+url.QueryEscape(raw))
		if strings.Contains(rec.Body.String(), `name="return_to"`) {
			t.Errorf("return_to=%q was carried into the panel", raw)
		}
	}
}

func commitForm(returnTo string) url.Values {
	form := url.Values{
		"text":               {"250ml water"},
		"items[0].kind":      {"water"},
		"items[0].source":    {"250ml water"},
		"items[0].include":   {"1"},
		"items[0].amount_ml": {"250"},
	}
	if returnTo != "" {
		form.Set("return_to", returnTo)
	}
	return form
}

func post(t *testing.T, h http.Handler, user users.User, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	req = req.WithContext(auth.ContextWithUser(req.Context(), user))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// From the dialog, a clean save sends the browser back to My Day, which
// re-renders with the water on it. On the capture page the receipt stays.
func TestCommitFromTheDialogGoesBackToTheDay(t *testing.T) {
	f := newFixture(t)
	h := router(f.svc)

	rec := post(t, h, f.user, "/app/capture/commit", commitForm("/app"))
	if rec.Code != http.StatusOK || rec.Header().Get("HX-Redirect") != "/app" {
		t.Fatalf("status %d, HX-Redirect %q; want 200 and /app", rec.Code, rec.Header().Get("HX-Redirect"))
	}

	day, err := f.hydration.Today(context.Background(), f.user)
	if err != nil || day.TotalML != 250 {
		t.Fatalf("hydration = %d (err %v), want 250", day.TotalML, err)
	}

	rec = post(t, h, f.user, "/app/capture/commit", commitForm(""))
	if rec.Header().Get("HX-Redirect") != "" {
		t.Error("the capture page itself was redirected away from its receipt")
	}
	if !strings.Contains(rec.Body.String(), "Logged 1 thing.") {
		t.Errorf("no receipt on the capture page: %s", rec.Body.String())
	}

	rec = post(t, h, f.user, "/app/capture/commit", commitForm("https://evil.example"))
	if got := rec.Header().Get("HX-Redirect"); got != "" {
		t.Errorf("redirected to %q from an unknown return_to", got)
	}
}

// A save with a failure in it stays on the receipt, embedded or not: reloading
// the day would hide the line that says what did not happen.
func TestCommitWithAFailureKeepsTheReceipt(t *testing.T) {
	f := newFixture(t)

	// A weight with no biometrics baseline is refused by the biometrics
	// service, so this receipt has one success and one failure.
	form := commitForm("/app")
	form.Set("items[1].kind", "weight")
	form.Set("items[1].source", "78kg")
	form.Set("items[1].include", "1")
	form.Set("items[1].kg", "78")

	rec := post(t, router(f.svc), f.user, "/app/capture/commit", form)
	if got := rec.Header().Get("HX-Redirect"); got != "" {
		t.Fatalf("redirected to %q although part of the save failed: %s", got, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Logged 1 of 2.") {
		t.Errorf("receipt missing: %s", rec.Body.String())
	}
}

// The app's htmx config never swaps a 5xx, so a failed read answered as 500
// would leave "Read it" looking dead. The panel explaining the failure has to
// arrive as a swappable response, with the sentence still in the box.
func TestAFailedReadStillShowsItsMessage(t *testing.T) {
	f := newFixture(t)
	f.parser.err = errors.New("model timed out")

	form := url.Values{"text": {"drank 400 ml of water"}, "return_to": {"/app"}}
	rec := post(t, meteredRouter(f), f.user, "/app/capture/parse", form)

	if rec.Code >= http.StatusInternalServerError {
		t.Fatalf("status = %d; htmx would drop the panel", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Something went wrong reading that.") {
		t.Errorf("error message missing: %s", body)
	}
	if !strings.Contains(body, "drank 400 ml of water") {
		t.Errorf("the sentence was lost: %s", body)
	}
}
