// Package theme carries how a page should look: light, dark, or following the
// device.
//
// It mirrors internal/shared/i18n on purpose. Appearance is resolved the way
// locale is — an account setting when somebody is signed in, a cookie for a
// visitor, a default for everyone else — and rides on the request context so a
// template can read it without being handed a request.
//
// Its own package, rather than part of internal/users, because the web layout,
// the auth middleware and the signed-out handlers all need it, and
// internal/users is the account record, not page chrome.
package theme

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"
)

// Appearance is how somebody wants the interface drawn.
//
// A closed set. An empty Appearance means "never chosen", which is what a NULL
// users.appearance column reads as; it is never rendered directly — Effective
// resolves it first.
type Appearance string

const (
	System Appearance = "system"
	Light  Appearance = "light"
	Dark   Appearance = "dark"
)

// DefaultAppearance is what the web renders for somebody who has never chosen.
//
// Dark, for now, because the application has only ever been drawn dark and
// light mode has not passed its legibility audit. When it has, this becomes
// System — and that is the whole change, because nothing stores the default:
// an unchosen account is NULL, and a visitor without a cookie has nothing.
//
// The web's default only. iOS reads a NULL as "follow the system" itself, which
// is why the API reports the stored value and never this one.
const DefaultAppearance = Dark

// Appearances is every choice, in the order a picker shows them.
var Appearances = []Appearance{System, Light, Dark}

// Valid reports whether a is one of the three choices. Empty is not valid.
func (a Appearance) Valid() bool {
	return slices.Contains(Appearances, a)
}

// Parse reads a submitted value. Case and surrounding space are forgiven; any
// other spelling is refused rather than guessed at, because there is no
// sensible near-miss for "light" or "dark".
func Parse(s string) (Appearance, bool) {
	a := Appearance(strings.ToLower(strings.TrimSpace(s)))
	return a, a.Valid()
}

// Effective is what to render: the stored choice when there is one, otherwise
// the cookie a visitor's switcher wrote, otherwise the default.
//
// The one place the order is decided. A signed-in account's own setting beats
// the cookie, so a shared laptop's last visitor does not decide how somebody
// else's account looks; but an account that has never chosen keeps whatever its
// owner picked on the landing page before signing up.
func Effective(stored, cookie Appearance) Appearance {
	switch {
	case stored.Valid():
		return stored
	case cookie.Valid():
		return cookie
	default:
		return DefaultAppearance
	}
}

// CookieName remembers a choice made with no account to store it on.
const CookieName = "appearance"

// cookieLifetime matches the locale cookie's: a visitor who chose light mode on
// the landing page has not changed their mind a week later.
const cookieLifetime = 365 * 24 * time.Hour

// FromCookie reads the visitor's choice, or "" when there is none or it is not
// one of the three.
func FromCookie(r *http.Request) Appearance {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return ""
	}
	a, ok := Parse(c.Value)
	if !ok {
		return ""
	}
	return a
}

// SetCookie remembers an explicit choice, with the same attributes as the
// locale cookie. An invalid value clears the cookie rather than storing
// something every later request would ignore.
//
// HttpOnly is safe here because nothing in the page reads the cookie: the
// server renders the class, and the switcher already knows what it just chose.
func SetCookie(w http.ResponseWriter, r *http.Request, a Appearance) {
	c := &http.Cookie{
		Name:     CookieName,
		Value:    string(a),
		Path:     "/",
		Expires:  time.Now().Add(cookieLifetime),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   servedOverHTTPS(r),
	}
	if !a.Valid() {
		c.Value = ""
		c.Expires = time.Unix(0, 0)
		c.MaxAge = -1
	}
	http.SetCookie(w, c)
}

type appearanceKey struct{}

// WithAppearance carries the appearance to render for this request.
func WithAppearance(ctx context.Context, a Appearance) context.Context {
	return context.WithValue(ctx, appearanceKey{}, a)
}

// AppearanceFrom returns the appearance to render, falling back to the default
// for a context nothing set — a page rendered outside the middleware stack, or
// a test — so the document is never drawn with no answer at all.
func AppearanceFrom(ctx context.Context) Appearance {
	if a, ok := ctx.Value(appearanceKey{}).(Appearance); ok && a.Valid() {
		return a
	}
	return DefaultAppearance
}

// servedOverHTTPS reports whether the visitor reached the site over HTTPS. In
// the cluster TLS ends at the ingress, so r.TLS is always nil there and the
// forwarded scheme is what says so. A forged header can only add Secure, which
// costs nothing.
func servedOverHTTPS(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}
