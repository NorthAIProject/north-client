package social_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/social"
)

// The whole way in, as a browser takes it: the link, a cookie, then the first
// page inside the app after signing up, whichever way they signed up.
func TestInviteLinkConnectsOnTheFirstPageInside(t *testing.T) {
	svc, _, _, pool := fixture(t)
	ctx := context.Background()
	ana := person(t, pool, "ana@north.test", "Ana")
	joao := person(t, pool, "joao@north.test", "João")
	invite, _ := svc.InviteFor(ctx, ana.ID, social.ChannelMessages)
	h := social.NewHandler(svc, "https://kheprios.com", true)

	public := chi.NewRouter()
	h.PublicRoutes(public)

	// Signed out: the page names the inviter and stores the code.
	rec := httptest.NewRecorder()
	public.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/i/"+invite.Code, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("landing = %d", rec.Code)
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == social.InviteCookie {
			cookie = c
		}
	}
	if cookie == nil || cookie.Value != invite.Code || !cookie.HttpOnly || !cookie.Secure {
		t.Fatalf("cookie = %+v", cookie)
	}

	// An unknown code is a plain 404.
	rec = httptest.NewRecorder()
	public.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/i/zzzzzzzzzz", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown code = %d, want 404", rec.Code)
	}

	// Signed up (any way), first page inside: connected, cookie cleared.
	req := httptest.NewRequest(http.MethodGet, "/app/today", nil)
	req.AddCookie(cookie)
	req = req.WithContext(auth.ContextWithUser(req.Context(), joao))
	rec = httptest.NewRecorder()
	h.RedeemInvite(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("page = %d", rec.Code)
	}
	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == social.InviteCookie && c.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("invite cookie not cleared")
	}
	o, err := svc.Overview(ctx, joao.ID)
	if err != nil || len(o.Following) != 1 || o.Following[0].ID != ana.ID {
		t.Fatalf("joao overview = %+v, %v", o, err)
	}
}

// Already signed in: the link connects at once and goes to Friends.
func TestInviteLinkWhenSignedInConnectsAtOnce(t *testing.T) {
	svc, _, _, pool := fixture(t)
	ctx := context.Background()
	ana := person(t, pool, "ana@north.test", "Ana")
	joao := person(t, pool, "joao@north.test", "João")
	invite, _ := svc.InviteFor(ctx, ana.ID, social.ChannelLink)

	public := chi.NewRouter()
	social.NewHandler(svc, "https://kheprios.com", false).PublicRoutes(public)
	req := httptest.NewRequest(http.MethodGet, "/i/"+invite.Code, nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), joao))
	rec := httptest.NewRecorder()
	public.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/app/friends" {
		t.Fatalf("got %d → %q", rec.Code, rec.Header().Get("Location"))
	}
	if o, _ := svc.Overview(ctx, ana.ID); len(o.Followers) != 1 {
		t.Fatalf("ana followers = %+v", o.Followers)
	}
}
