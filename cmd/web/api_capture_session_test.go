package main

import (
	"encoding/json"
	"net/http"
	"testing"
)

// The app's App Intents capture with the sign-in session, not an nk_ token.
// The same two calls must work, and write to the signed-in person.
func TestCaptureAPIAcceptsTheAppSession(t *testing.T) {
	api := newAPIClient(t)

	// The test model cannot read a sentence, so parse only has to get past
	// the authenticator here; commit below proves the writes land.
	if rec := api.call(http.MethodPost, "/api/v1/capture/parse", `{"text":"drank 500ml water"}`); rec.Code == http.StatusUnauthorized {
		t.Fatalf("parse refused the session: %s", rec.Body)
	}

	rec := api.call(http.MethodPost, "/api/v1/capture/commit",
		`{"items":[{"kind":"water","source":"500ml water","water":{"amount_ml":500}}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("commit: %d %s", rec.Code, rec.Body)
	}
	var result struct{ Written, Failed int }
	_ = json.Unmarshal(rec.Body.Bytes(), &result)
	if result.Written != 1 || result.Failed != 0 {
		t.Fatalf("commit = %+v, want one written: %s", result, rec.Body)
	}

	var care struct {
		Water struct {
			TotalML int `json:"totalMl"`
		} `json:"water"`
	}
	body := api.call(http.MethodGet, "/api/v1/care", "").Body.Bytes()
	_ = json.Unmarshal(body, &care)
	if care.Water.TotalML != 500 {
		t.Fatalf("care water today = %d, want 500: %s", care.Water.TotalML, body)
	}
}

// A token that is neither a connection token nor a live session stays out.
func TestCaptureAPIRefusesAnUnknownToken(t *testing.T) {
	api := newAPIClient(t)
	api.bearer = "Bearer not-a-session"
	if rec := api.call(http.MethodPost, "/api/v1/capture/parse", `{"text":"2L water"}`); rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
