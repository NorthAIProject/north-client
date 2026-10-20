package moderation_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/moderation"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

func TestOpsQueueOperatorsOnly(t *testing.T) {
	pool := testdb.New(t)
	op := person(t, pool, "Operator")
	someone := person(t, pool, "Someone")
	svc := moderation.NewService(pool).WithOperators([]uuid.UUID{op}, nil)
	r := chi.NewRouter()
	moderation.NewHandler(svc).Routes(r)

	get := func(as uuid.UUID) int {
		req := httptest.NewRequest(http.MethodGet, "/ops/moderation", nil)
		req = req.WithContext(auth.ContextWithUser(req.Context(), users.User{ID: as, DisplayName: "X"}))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := get(someone); code != http.StatusNotFound {
		t.Fatalf("a non-operator got %d; want 404", code)
	}
	if code := get(op); code != http.StatusOK {
		t.Fatalf("an operator got %d; want 200", code)
	}
}

// Accepting the terms sends you back where you were, never off-site.
func TestAcceptTermsRedirectsLocally(t *testing.T) {
	pool := testdb.New(t)
	ana := person(t, pool, "Ana")
	svc := moderation.NewService(pool)
	r := chi.NewRouter()
	moderation.NewHandler(svc).Routes(r)

	post := func(next string) string {
		form := url.Values{"next": {next}}
		req := httptest.NewRequest(http.MethodPost, "/moderation/terms", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req = req.WithContext(auth.ContextWithUser(req.Context(), users.User{ID: ana}))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("status %d", rec.Code)
		}
		return rec.Header().Get("Location")
	}
	if got := post("/app/crews/x"); got != "/app/crews/x" {
		t.Fatalf("local next went to %q", got)
	}
	if got := post("//evil.example"); got != "/app" {
		t.Fatalf("off-site next went to %q", got)
	}
	ok, err := svc.TermsAccepted(t.Context(), ana)
	if err != nil || !ok {
		t.Fatalf("terms accepted = %v, %v", ok, err)
	}
}
