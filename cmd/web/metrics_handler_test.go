package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v5"
)

// The auth check runs before the pool is touched, so a nil pool is enough to
// prove every rejection path. The success path needs Postgres.
func TestMetricsHandlerRejectsBadTokens(t *testing.T) {
	e := echo.New()
	e.GET("/internal/metrics", metricsHandler(nil, "correct-horse-battery-staple"))

	cases := map[string]string{
		"missing header": "",
		"wrong scheme":   "Basic correct-horse-battery-staple",
		"wrong token":    "Bearer wrong",
		"prefix only":    "Bearer correct-horse",
	}
	for name, header := range cases {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/internal/metrics", nil)
			if header != "" {
				r.Header.Set("Authorization", header)
			}
			w := httptest.NewRecorder()
			e.ServeHTTP(w, r)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("%s: got %d, want 401", name, w.Code)
			}
		})
	}
}
