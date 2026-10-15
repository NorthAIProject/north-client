package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/shared/theme"
	"github.com/NorthAIProject/north-client/internal/users"
)

// What LoadUser leaves on the context for a signed-in request carrying the
// given visitor cookie, after middleware.Appearance has run first as it does in
// the router.
func signedInAppearance(t *testing.T, stored *theme.Appearance, cookie string) theme.Appearance {
	t.Helper()
	svc, sessions, pool, _ := newService(t)
	ctx := context.Background()

	user, _, err := svc.Signup(ctx, validSignup(), auth.Metadata{})
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	if stored != nil {
		if _, err = users.NewService(users.NewRepository(pool)).SetAppearance(ctx, user.ID, string(*stored), "test"); err != nil {
			t.Fatalf("set appearance: %v", err)
		}
	}
	token, _, err := sessions.Create(ctx, user.ID, auth.Metadata{})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/app", nil)
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: token})
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: theme.CookieName, Value: cookie})
	}

	var got theme.Appearance
	mw := auth.NewMiddleware(sessions, false, nil)
	h := middleware.Appearance(mw.LoadUser(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if _, ok := auth.UserFrom(r.Context()); !ok {
			t.Fatal("the session did not resolve")
		}
		got = theme.AppearanceFrom(r.Context())
	})))
	h.ServeHTTP(httptest.NewRecorder(), req)
	return got
}

// The account's own choice beats whatever the browser's cookie says: a shared
// laptop's last visitor must not decide how somebody else's account looks.
func TestTheAccountsAppearanceBeatsTheCookie(t *testing.T) {
	dark := theme.Dark
	if got := signedInAppearance(t, &dark, "light"); got != theme.Dark {
		t.Fatalf("appearance = %q, want the account's dark", got)
	}
}

// An account that never chose keeps what its owner picked on the landing page.
func TestAnAccountThatNeverChoseKeepsTheCookie(t *testing.T) {
	if got := signedInAppearance(t, nil, "light"); got != theme.Light {
		t.Fatalf("appearance = %q, want the cookie's light", got)
	}
}

func TestAnAccountThatNeverChoseWithNoCookieGetsTheDefault(t *testing.T) {
	if got := signedInAppearance(t, nil, ""); got != theme.DefaultAppearance {
		t.Fatalf("appearance = %q, want the default", got)
	}
}
