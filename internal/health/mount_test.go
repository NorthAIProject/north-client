package health_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/health"
)

// The handler reads the provider out of the request path, so it only works
// mounted behind something that strips the prefix first. cmd/web does that with
// chi.Mount plus http.StripPrefix; this pins that combination, because getting
// it wrong files every reading under a source named "ingest" — or under nothing
// at all — and the handler tests alone would not notice.
func TestMountedBehindAPrefixTheSourceStillResolves(t *testing.T) {
	svc, user := newService(t)

	r := chi.NewRouter()
	r.Mount("/ingest/health", http.StripPrefix("/ingest/health", health.NewHandler(health.HandlerConfig{
		Service: svc,
		Auth:    stubAuth{token: "nk_testtoken", user: user},
	})))

	req := httptest.NewRequest(http.MethodPost, "/ingest/health/apple_health", strings.NewReader(
		`{"readings":[{"metric":"steps","value":8000,"unit":"count","started_at":"2026-08-15T02:00:00Z"}]}`))
	req.Header.Set("Authorization", "Bearer nk_testtoken")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	stored, err := svc.Between(context.Background(), user.ID, "steps",
		at("2026-08-15T00:00:00Z"), at("2026-08-16T00:00:00Z"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("got %d readings, want 1", len(stored))
	}
	if stored[0].Source != "apple_health" {
		t.Errorf("Source = %q, want %q — the prefix was not stripped cleanly", stored[0].Source, "apple_health")
	}
}
