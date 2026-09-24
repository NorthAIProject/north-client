package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

type fakeSessionResolver struct {
	session auth.Session
	err     error
	token   string
}

func (f fakeSessionResolver) Resolve(_ context.Context, token string) (auth.Session, error) {
	if f.token != token {
		return auth.Session{}, apperr.ErrUnauthenticated
	}
	return f.session, f.err
}

func testRouter(resolver auth.SessionResolver) http.Handler {
	r := chi.NewRouter()
	auth.NewAPI(resolver).Routes(r)
	return r
}

func TestMeRejectsMissingBearerTokenAsJSON(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/me", nil)
	recorder := httptest.NewRecorder()

	testRouter(fakeSessionResolver{}).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("content type = %q, want JSON", got)
	}
}

func TestMeReturnsPublicUserProjection(t *testing.T) {
	id := uuid.New()
	resolver := fakeSessionResolver{
		token: "session-token",
		session: auth.Session{
			User: users.User{
				ID:          id,
				Email:       "fernando@example.com",
				DisplayName: "Fernando",
				Timezone:    "America/Sao_Paulo",
			},
			ExpiresAt: time.Now().Add(time.Hour),
		},
	}
	request := httptest.NewRequest(http.MethodGet, "/me", nil)
	request.Header.Set("Authorization", "Bearer session-token")
	recorder := httptest.NewRecorder()

	testRouter(resolver).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body struct {
		User struct {
			ID              string `json:"id"`
			Email           string `json:"email"`
			DisplayName     string `json:"displayName"`
			Timezone        string `json:"timezone"`
			NeedsOnboarding bool   `json:"needsOnboarding"`
		} `json:"user"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.User.ID != id.String() || body.User.Email != "fernando@example.com" || body.User.DisplayName != "Fernando" || body.User.Timezone != "America/Sao_Paulo" {
		t.Fatalf("unexpected user projection: %+v", body.User)
	}
	if !body.User.NeedsOnboarding {
		t.Fatal("needsOnboarding = false, want true for a new user")
	}
}

func TestMeReturnsGenericJSONForResolverFailure(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/me", nil)
	request.Header.Set("Authorization", "Bearer session-token")
	recorder := httptest.NewRecorder()

	resolver := fakeSessionResolver{token: "session-token", err: errors.New("database unavailable")}
	testRouter(resolver).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if got := recorder.Body.String(); got == "" || contains(got, "database unavailable") {
		t.Fatalf("response leaked resolver error: %s", got)
	}
}

func contains(value, part string) bool {
	return len(value) >= len(part) && strings.Contains(value, part)
}
