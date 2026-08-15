package health_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/health"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// stubAuth resolves exactly one token, so a test can tell "the endpoint
// rejected me" apart from "the endpoint never asked".
type stubAuth struct {
	token string
	user  users.User
}

func (a stubAuth) Authenticate(_ context.Context, token string) (users.User, error) {
	if token != a.token {
		return users.User{}, apperr.ErrUnauthenticated
	}
	return a.user, nil
}

func newHandler(t *testing.T) (http.Handler, *health.Service, users.User) {
	t.Helper()

	svc, user := newService(t)
	h := health.NewHandler(health.HandlerConfig{
		Service: svc,
		Auth:    stubAuth{token: "nk_testtoken", user: user},
	})
	return h, svc, user
}

func post(t *testing.T, h http.Handler, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestValidPayloadIsStoredUnderTheSourceInThePath(t *testing.T) {
	h, svc, user := newHandler(t)

	rec := post(t, h, "/apple_health", "nk_testtoken", `{"readings":[
		{"metric":"hrv_sdnn","value":47.5,"unit":"ms","started_at":"2026-08-15T02:00:00Z"}
	]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}

	var got struct {
		Written int `json:"written"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v (body: %s)", err, rec.Body.String())
	}
	if got.Written != 1 {
		t.Errorf("written = %d, want 1", got.Written)
	}

	stored, err := svc.Between(context.Background(), user.ID, "hrv_sdnn",
		at("2026-08-15T00:00:00Z"), at("2026-08-16T00:00:00Z"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("got %d stored readings, want 1", len(stored))
	}
	if stored[0].Source != "apple_health" {
		t.Errorf("Source = %q, want %q — the path segment is what names the provider",
			stored[0].Source, "apple_health")
	}
}

// The endpoint sits outside CSRF and session auth, so the token is the only
// thing standing in front of a write.
func TestPayloadWithoutAValidTokenIsRejectedAndStoresNothing(t *testing.T) {
	h, svc, user := newHandler(t)

	body := `{"readings":[{"metric":"steps","value":99,"unit":"count","started_at":"2026-08-15T02:00:00Z"}]}`

	for _, tc := range []struct {
		name  string
		token string
	}{
		{"no token", ""},
		{"wrong token", "nk_notthistoken"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := post(t, h, "/apple_health", tc.token, body)
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", rec.Code)
			}
		})
	}

	stored, err := svc.Between(context.Background(), user.ID, "steps",
		at("2026-08-15T00:00:00Z"), at("2026-08-16T00:00:00Z"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(stored) != 0 {
		t.Errorf("got %d stored readings, want 0 — an unauthenticated write landed", len(stored))
	}
}

// A source is a durable identifier: it names rows for as long as they exist and
// is what a person later disconnects. Junk in that column cannot be typed away.
func TestMalformedSourceIsRejected(t *testing.T) {
	h, _, _ := newHandler(t)

	body := `{"readings":[{"metric":"steps","value":1,"unit":"count","started_at":"2026-08-15T02:00:00Z"}]}`

	// Every case here is a path a real client could actually send: a raw space
	// is not, because httptest refuses to build it and a server would never
	// receive it undecoded.
	for _, path := range []string{"/", "/Apple_Health", "/apple%2Fhealth", "/a", "/" + strings.Repeat("a", 100)} {
		rec := post(t, h, path, "nk_testtoken", body)
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
			t.Errorf("path %q: status = %d, want 400 or 404", path, rec.Code)
		}
	}
}

func TestMalformedJSONIsRejected(t *testing.T) {
	h, _, _ := newHandler(t)

	rec := post(t, h, "/apple_health", "nk_testtoken", `{"readings":[{"metric":`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// The validation rules live in the service; this only proves the handler
// reports them as the caller's fault rather than as a server error.
func TestAnInvalidReadingIsReportedAsABadRequest(t *testing.T) {
	h, _, _ := newHandler(t)

	rec := post(t, h, "/apple_health", "nk_testtoken",
		`{"readings":[{"metric":"","value":1,"unit":"count","started_at":"2026-08-15T02:00:00Z"}]}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestGetIsNotAllowed(t *testing.T) {
	h, _, _ := newHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/apple_health", nil)
	req.Header.Set("Authorization", "Bearer nk_testtoken")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}
