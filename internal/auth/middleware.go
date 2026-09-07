package auth

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/users"
)

// SessionCookieName holds the raw session token.
const SessionCookieName = "north_session"

// userContextKey is unexported so only this package can put a user into the
// context. Any other package could otherwise forge an authenticated request.
type userContextKey struct{}

// Middleware resolves sessions and guards routes.
type Middleware struct {
	sessions *SessionStore
	secure   bool
	trusted  middleware.TrustedProxies
}

// NewMiddleware builds the auth middleware. secure should be true in
// production; it is false locally because http://localhost cannot set Secure
// cookies.
//
// trusted decides whether X-Forwarded-For may be believed when recording where
// a session was created from. Empty means record the peer address.
func NewMiddleware(sessions *SessionStore, secure bool, trusted middleware.TrustedProxies) *Middleware {
	return &Middleware{sessions: sessions, secure: secure, trusted: trusted}
}

// LoadUser attaches the signed-in user to the request context when there is a
// valid session, and does nothing otherwise.
//
// It never rejects a request. Pages that work signed-out but render differently
// signed-in (the landing page, a shared link) need this without RequireAuth.
func (m *Middleware) LoadUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(SessionCookieName)
		if err != nil || cookie.Value == "" {
			next.ServeHTTP(w, r)
			return
		}

		session, err := m.sessions.Resolve(r.Context(), cookie.Value)
		if err != nil {
			// Expired or unknown token: clear the stale cookie so the browser
			// stops presenting it on every subsequent request.
			if apperr.Is(err, apperr.ErrUnauthenticated) {
				m.ClearCookie(w)
			}
			next.ServeHTTP(w, r)
			return
		}

		// The locale rides along with the user rather than in its own
		// middleware: it is a property of the account, and a second pass would
		// have to resolve the session again to find it.
		ctx := ContextWithUser(r.Context(), session.User)
		ctx = i18n.WithLocale(ctx, string(session.User.Locale))

		// The same string the server sends as PostHog's DistinctId. Without it
		// the browser's anonymous session and the account are two unrelated
		// people, and no funnel crosses the signup.
		ctx = middleware.WithIdentity(ctx, session.User.ID.String())

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAuth rejects requests with no signed-in user. It must be mounted after
// LoadUser.
func (m *Middleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := UserFrom(r.Context()); ok {
			next.ServeHTTP(w, r)
			return
		}

		// HTMX follows a client-side redirect header; a normal navigation needs
		// a real 303. Returning a 303 to HTMX would swap the login page into
		// whatever fragment triggered the request.
		if r.Header.Get("HX-Request") == "true" {
			w.Header().Set("HX-Redirect", loginRedirect(r))
			w.WriteHeader(http.StatusOK)
			return
		}

		http.Redirect(w, r, loginRedirect(r), http.StatusSeeOther)
	})
}

// ContextWithUser carries a signed-in user. LoadUser is the only caller in the
// application; it is exported so a template that changes shape for a signed-in
// visitor can be tested without standing up a session and a database.
func ContextWithUser(ctx context.Context, u users.User) context.Context {
	return context.WithValue(ctx, userContextKey{}, u)
}

// UserFrom returns the signed-in user, if any.
func UserFrom(ctx context.Context) (users.User, bool) {
	u, ok := ctx.Value(userContextKey{}).(users.User)
	return u, ok
}

// MustUser returns the signed-in user and panics if there is none. Safe only
// inside a handler mounted behind RequireAuth, where the panic would mean the
// route was mounted wrong.
func MustUser(ctx context.Context) users.User {
	u, ok := UserFrom(ctx)
	if !ok {
		panic("auth: no user in context; route is missing RequireAuth")
	}
	return u
}

// SetCookie writes the session cookie.
func (m *Middleware) SetCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		Secure:   m.secure,
		// Lax rather than Strict: Strict would leave a user signed out when
		// arriving from an external link, including the ones North itself sends
		// by email or Telegram.
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearCookie removes the session cookie.
func (m *Middleware) ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   m.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// RequestMetadata captures the client details stored alongside a session.
//
// A method rather than a function because the address is only meaningful
// against the trusted-proxy list: see clientIP.
func (m *Middleware) RequestMetadata(r *http.Request) Metadata {
	return Metadata{UserAgent: r.UserAgent(), IP: middleware.ClientIP(r, m.trusted)}
}

// loginRedirect sends the user to the login page, remembering where they were
// headed so they land there after signing in.
//
// Only the path and query are preserved. Echoing a caller-supplied absolute URL
// would make this an open redirect.
func loginRedirect(r *http.Request) string {
	if r.Method != http.MethodGet {
		return "/login"
	}
	next := r.URL.RequestURI()
	if !SafeRedirect(next) || next == "/" {
		return "/login"
	}
	return "/login?next=" + url.QueryEscape(next)
}

// SafeRedirect reports whether a post-login destination is a local path.
//
// Anything absolute, scheme-relative ("//evil.com", which browsers resolve as a
// different host), or backslash-prefixed is rejected. Without this check the
// `next` parameter is an open redirect, which turns North's own login page into
// a convincing launchpad for phishing.
func SafeRedirect(next string) bool {
	if next == "" || !strings.HasPrefix(next, "/") {
		return false
	}
	if strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
		return false
	}
	// A parseable path with no host is the only acceptable shape.
	u, err := url.Parse(next)
	return err == nil && u.Scheme == "" && u.Host == ""
}

// clientIP is deliberately absent.
//
// It used to take the leftmost X-Forwarded-For entry, which is precisely the
// part a client writes: a proxy appends the address it observed to the right of
// whatever arrived. So the IP recorded against every session was chosen by
// whoever created it, and a person reviewing where their account had been
// signed in from was reading a value an attacker supplied. Sessions now record
// middleware.ClientIP, which consults the header only when the peer is a
// trusted proxy and reads the hops from the right.
