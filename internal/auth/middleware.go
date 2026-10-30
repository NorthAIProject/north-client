package auth

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/users"
)

// userContextKey is unexported so only this package can put a user into the
// context. Any other package could otherwise forge an authenticated request.
type userContextKey struct{}

// Middleware records where a session was created from.
type Middleware struct {
	clientIP echo.IPExtractor
}

// NewMiddleware builds the auth middleware.
//
// trusted decides whether X-Forwarded-For may be believed when recording where
// a session was created from. Empty means record the peer address.
func NewMiddleware(trusted middleware.TrustedProxies) *Middleware {
	return &Middleware{clientIP: trusted.IPExtractor()}
}

// ContextWithUser carries a signed-in user. Bearer is the only caller in the
// application; it is exported so a handler can be tested without standing up a
// session and a database.
func ContextWithUser(ctx context.Context, u users.User) context.Context {
	return context.WithValue(ctx, userContextKey{}, u)
}

// UserFrom returns the signed-in user, if any.
func UserFrom(ctx context.Context) (users.User, bool) {
	u, ok := ctx.Value(userContextKey{}).(users.User)
	return u, ok
}

// MustUser returns the signed-in user and panics if there is none. Safe only
// inside a handler mounted behind Bearer, where the panic would mean the route
// was mounted wrong.
func MustUser(ctx context.Context) users.User {
	u, ok := UserFrom(ctx)
	if !ok {
		panic("auth: no user in context; route is missing RequireAuth")
	}
	return u
}

// RequestMetadata captures the client details stored alongside a session.
//
// A method rather than a function because the address is only meaningful
// against the trusted-proxy list: see clientIP.
//
// A Middleware built without NewMiddleware trusts no proxy and records the
// peer address, as an empty TrustedProxies would.
func (m *Middleware) RequestMetadata(r *http.Request) Metadata {
	clientIP := m.clientIP
	if clientIP == nil {
		clientIP = echo.ExtractIPDirect()
	}
	return Metadata{UserAgent: r.UserAgent(), IP: clientIP(r)}
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
// TrustedProxies.IPExtractor, which consults the header only when the peer is a
// trusted proxy and reads the hops from the right.
