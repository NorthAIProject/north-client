package social_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/social"
	"github.com/NorthAIProject/north-client/internal/social/facebook"
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

// The web flow proves the callback belongs to a flow this browser started:
// a state that does not match the cookie connects nothing.
func TestFacebookCallbackChecksTheStateCookie(t *testing.T) {
	svc, _, _, pool := fixture(t)
	me := person(t, pool, "me@north.test", "Me")
	svc.WithFacebook(&facebookFake{accounts: map[string]facebook.Account{"code": {ID: "fb-me"}}})
	h := social.NewHandler(svc, "https://kheprios.com", true)
	r := chi.NewRouter()
	r.Route("/app", h.Routes)
	serve := func(target string, cookie *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		req = req.WithContext(auth.ContextWithUser(req.Context(), me))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	rec := serve("/app/friends/facebook/connect", nil)
	consent, _ := url.Parse(rec.Header().Get("Location"))
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "north_facebook_oauth" {
			cookie = c
		}
	}
	if rec.Code != http.StatusFound || consent.Host != "facebook.test" || cookie == nil || !cookie.HttpOnly || !cookie.Secure ||
		cookie.Value != consent.Query().Get("state") || consent.Query().Get("redirect_uri") != "https://kheprios.com"+social.FacebookWebCallbackPath {
		t.Fatalf("connect = %d %s, cookie %+v", rec.Code, consent, cookie)
	}

	for name, c := range map[string]*http.Cookie{"no cookie": nil, "another flow's cookie": {Name: cookie.Name, Value: "someone-elses"}} {
		rec = serve("/app/friends/facebook/callback?code=code&state="+url.QueryEscape(cookie.Value), c)
		if loc := rec.Header().Get("Location"); loc != "/app/friends?facebook=expired#facebook" {
			t.Fatalf("%s: redirected to %q", name, loc)
		}
	}
	if got, _ := svc.FacebookFriends(context.Background(), me.ID); got.Connected {
		t.Fatal("connected without a matching state")
	}

	rec = serve("/app/friends/facebook/callback?code=code&state="+url.QueryEscape(cookie.Value), cookie)
	if loc := rec.Header().Get("Location"); loc != "/app/friends?facebook=connected#facebook" {
		t.Fatalf("matching state: redirected to %q", loc)
	}
	if got, _ := svc.FacebookFriends(context.Background(), me.ID); !got.Connected {
		t.Fatal("not connected after a matching state")
	}
}

// The app's callback always lands back in the app, saying how it went.
func TestNativeFacebookCallbackRedirectsToTheApp(t *testing.T) {
	svc, _, _, _ := fixture(t)
	svc.WithFacebook(&facebookFake{})
	r := chi.NewRouter()
	social.NewAPI(svc, "https://kheprios.com").PublicRoutes(r)
	for query, want := range map[string]string{
		"error=access_denied&state=x": "cancelled",
		"state=unknown&code=code":     "expired",
	} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/social/facebook/callback?"+query, nil))
		if loc := rec.Header().Get("Location"); rec.Code != http.StatusFound || loc != social.FacebookReturnURL+"?result="+want {
			t.Fatalf("%s: %d %q, want result=%s", query, rec.Code, loc, want)
		}
	}
}
