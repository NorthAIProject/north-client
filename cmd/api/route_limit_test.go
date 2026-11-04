package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/config"
)

// Composition, on the server as routes() builds it: the bearer groups are bounded
// and share one allowance, and nothing outside them is touched by it. Only
// tokenless requests are spent, so no session query runs and the real limit can
// be walked quickly.
func TestTheBearerLimitCoversBearerRoutesAndNothingElse(t *testing.T) {
	// Thousands of request log lines would slow the walk enough for the bucket to
	// refill noticeably, on top of burying any failure.
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	handler := testRoutesWith(t, func(cfg *config.Config) {
		cfg.Telegram = config.TelegramConfig{BotToken: "test-token", WebhookSecret: "test-secret"}
	})

	do := func(method, path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
		return rec
	}

	// Most of the allowance through the upload group. If each group counted for
	// itself, the JSON group would then still have all of its own to spend.
	spent := auth.DefaultBearerRequestsPerAddress * 3 / 4
	for i := range spent {
		if rec := do(http.MethodPost, "/api/v1/knowledge/uploads"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("upload request %d: status = %d, want 401", i+1, rec.Code)
		}
	}

	// The bucket refills as this runs, so walk to the refusal rather than count.
	jsonRequests := 0
	for do(http.MethodGet, "/api/v1/goals").Code == http.StatusUnauthorized {
		jsonRequests++
		if jsonRequests > auth.DefaultBearerRequestsPerAddress {
			t.Fatal("never refused, or the JSON group has an allowance of its own")
		}
	}
	if jsonRequests >= auth.DefaultBearerRequestsPerAddress {
		t.Errorf("the JSON group took %d requests to refuse: uploads did not spend the same allowance", jsonRequests)
	}

	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/goals"},
		{http.MethodPost, "/api/v1/knowledge/uploads"},
		{http.MethodPost, "/api/v1/conversations/x/reply"},
	} {
		rec := do(route.method, route.path)
		if rec.Code != http.StatusTooManyRequests {
			// One token may have refilled since the walk stopped; ask once more.
			rec = do(route.method, route.path)
		}
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("%s past the bound: status = %d, want 429", route.path, rec.Code)
		}
		if rec.Header().Get("Retry-After") == "" {
			t.Errorf("%s: a 429 with no Retry-After", route.path)
		}
	}

	// The same address, with its bearer allowance spent.
	for _, route := range []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/livez", http.StatusOK},
		{http.MethodGet, "/healthz", http.StatusOK},
		{http.MethodPost, "/mcp", http.StatusUnauthorized},
		{http.MethodPost, "/ingest/health", http.StatusUnauthorized},
		{http.MethodPost, "/webhooks/telegram", http.StatusUnauthorized},
		// Signing in has its own throttle, and has to keep working for somebody
		// whose app is in a retry loop.
		{http.MethodGet, "/api/v1/auth/options", http.StatusOK},
	} {
		if rec := do(route.method, route.path); rec.Code != route.want {
			t.Errorf("%s %s: status = %d, want %d", route.method, route.path, rec.Code, route.want)
		}
	}
}
