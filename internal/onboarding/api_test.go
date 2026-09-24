package onboarding_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/onboarding"
)

type fakeSessionResolver struct{}

func (fakeSessionResolver) Resolve(context.Context, string) (auth.Session, error) {
	return auth.Session{}, nil
}

func TestOnboardingRejectsMissingBearerTokenAsJSON(t *testing.T) {
	r := chi.NewRouter()
	onboarding.NewAPI(nil, fakeSessionResolver{}).Routes(r)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/onboarding", nil)
	r.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("content type = %q, want JSON", got)
	}
}
