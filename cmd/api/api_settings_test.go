package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/config"
)

type apiClient struct {
	t       *testing.T
	handler http.Handler
	bearer  string
}

func (c apiClient) call(method, path, body string) *httptest.ResponseRecorder {
	c.t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", c.bearer)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c.handler.ServeHTTP(rec, req)
	return rec
}

func newAPIClient(t *testing.T) apiClient {
	handler, pool := testRoutesAndPool(t, func(*config.Config) {})
	return apiClient{t: t, handler: handler, bearer: "Bearer " + signIn(t, pool).Value}
}

func TestSettingsAPIProfileRoundTrips(t *testing.T) {
	api := newAPIClient(t)

	rec := api.call(http.MethodPut, "/api/v1/settings/profile",
		`{"displayName":"Ada L.","timezone":"Europe/Lisbon","locale":"en","coachingStyle":"Ask questions. Help me think it through.","coachingTone":"warm"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("put profile: %d %s", rec.Code, rec.Body)
	}

	var got struct{ DisplayName, Timezone, CoachingTone string }
	_ = json.Unmarshal(api.call(http.MethodGet, "/api/v1/settings/profile", "").Body.Bytes(), &got)
	if got.DisplayName != "Ada L." || got.Timezone != "Europe/Lisbon" || got.CoachingTone != "warm" {
		t.Fatalf("profile after save = %+v", got)
	}
}

// A connection's token exists once: in the response that created it. The list
// that follows must not carry it, or any later reader could replay it.
func TestSettingsAPIShowsAConnectionTokenOnce(t *testing.T) {
	api := newAPIClient(t)

	rec := api.call(http.MethodPost, "/api/v1/settings/connections", `{"name":"Laptop","kind":"claude_code"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	var created struct {
		Token      string
		Connection struct{ ID string }
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.Token == "" {
		t.Fatal("no token in the creating response")
	}

	list := api.call(http.MethodGet, "/api/v1/settings/connections", "").Body.String()
	if strings.Contains(list, created.Token) {
		t.Fatal("the connection list repeats the token")
	}

	if rec := api.call(http.MethodDelete, "/api/v1/settings/connections/"+created.Connection.ID, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body)
	}
}

// Deleting needs the account's own email typed back, and afterwards the
// session that asked is gone with it.
func TestSettingsAPIDeletesAnAccountOnlyWhenConfirmed(t *testing.T) {
	api := newAPIClient(t)

	if rec := api.call(http.MethodPost, "/api/v1/account/delete", `{"confirmEmail":"someone@else.test"}`); rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("wrong email: %d, want 422", rec.Code)
	}
	if rec := api.call(http.MethodPost, "/api/v1/account/delete", `{"confirmEmail":"ada@north.test"}`); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body)
	}
	if rec := api.call(http.MethodGet, "/api/v1/me", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("after deletion /me = %d, want 401", rec.Code)
	}
}
