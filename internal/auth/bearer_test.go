package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

type stubSessions struct{ user users.User }

func (s stubSessions) Resolve(context.Context, string) (Session, error) {
	return Session{User: s.user}, nil
}

// The app sends the phone's Accept-Language on every request; once signed in,
// the account's own language must win, exactly as it does in the browser.
func TestRequireBearerUsesTheAccountLocale(t *testing.T) {
	t.Parallel()

	var got string
	h := RequireBearer(stubSessions{user: users.User{Locale: users.LocalePTBR}})(
		http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			got = i18n.LocaleFrom(r.Context())
		}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Accept-Language", "en-GB")
	req = req.WithContext(i18n.WithLocale(req.Context(), "en"))
	h.ServeHTTP(httptest.NewRecorder(), req)

	if got != string(users.LocalePTBR) {
		t.Fatalf("locale = %q, want %q", got, users.LocalePTBR)
	}
}
