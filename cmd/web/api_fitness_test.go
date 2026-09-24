package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NorthAIProject/north-client/internal/config"
)

// Strava's return always lands back in the app with a result it can show. A
// state the server never issued is "expired", never "connected".
func TestStravaNativeCallbackAlwaysReturnsToTheApp(t *testing.T) {
	handler, _ := testRoutesAndPool(t, func(*config.Config) {})

	for query, want := range map[string]string{
		"?error=access_denied&state=x": "khepri://fitness/strava?result=cancelled",
		"?state=never-issued&code=abc": "khepri://fitness/strava?result=expired",
		"?code=abc":                    "khepri://fitness/strava?result=expired",
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/fitness/strava/callback"+query, nil))
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != want {
			t.Errorf("%s → %d %q, want 302 %q", query, rec.Code, rec.Header().Get("Location"), want)
		}
	}
}
