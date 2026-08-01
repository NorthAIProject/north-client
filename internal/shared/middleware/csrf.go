package middleware

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
)

const (
	// CSRFCookieName holds the token. It is deliberately readable by the
	// browser's form-rendering path only through the server: we never need
	// JavaScript to read it, so it stays HttpOnly.
	CSRFCookieName = "north_csrf"

	// CSRFFieldName is the hidden form input carrying the token.
	CSRFFieldName = "csrf_token"

	// CSRFHeaderName carries the token for HTMX and fetch requests.
	CSRFHeaderName = "X-CSRF-Token"

	csrfTokenBytes = 32
)

var csrfKey = ctxValue[string]{key: "csrf_token"}

// CSRF implements double-submit cookie protection.
//
// The server issues a random token in an HttpOnly cookie and embeds the same
// value in every form it renders. A state-changing request must present both,
// and they must match. A cross-site attacker can cause the browser to send the
// cookie but cannot read it, so cannot put a matching value in the body.
//
// Safe methods pass through untouched but still get a token issued, so that the
// form rendered by a GET has one to embed.
//
// secure should be true in production; it is false in development because
// http://localhost cannot set Secure cookies.
func CSRF(secure bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, err := ensureCSRFCookie(w, r, secure)
			if err != nil {
				http.Error(w, "Could not establish a secure session.", http.StatusInternalServerError)
				return
			}

			ctx := csrfKey.set(r.Context(), token)

			if isSafeMethod(r.Method) {
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Origin and Referer are set by the browser and cannot be forged by
			// page JavaScript. Checking them first rejects the common case
			// cheaply and catches attacks that manage to replay a stale token.
			if !sameOrigin(r) {
				http.Error(w, "Request origin not allowed.", http.StatusForbidden)
				return
			}

			presented := r.Header.Get(CSRFHeaderName)
			if presented == "" {
				// ParseForm is safe to call here: handlers that need the body
				// call it again and get the cached result.
				if err := r.ParseForm(); err == nil {
					presented = r.PostFormValue(CSRFFieldName)
				}
			}

			// Constant time: a byte-by-byte comparison would leak the token
			// one character at a time to an attacker who can measure timing.
			if subtle.ConstantTimeCompare([]byte(presented), []byte(token)) != 1 {
				http.Error(w, "Invalid or missing CSRF token. Please reload the page and try again.", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// CSRFToken returns the token for the current request, for embedding in a form.
func CSRFToken(ctx context.Context) string {
	token, _ := csrfKey.get(ctx)
	return token
}

func ensureCSRFCookie(w http.ResponseWriter, r *http.Request, secure bool) (string, error) {
	if c, err := r.Cookie(CSRFCookieName); err == nil && len(c.Value) >= 32 {
		return c.Value, nil
	}

	raw := make([]byte, csrfTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)

	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		// Session cookie: it lives as long as the browser window, which is
		// long enough for any form and short enough to limit replay.
	})

	return token, nil
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

// sameOrigin reports whether the request appears to come from this site.
//
// A request with neither Origin nor Referer is accepted: some privacy tools
// strip both, and rejecting them would break those users. The token check still
// applies, so this is a defence in depth rather than the only barrier.
func sameOrigin(r *http.Request) bool {
	host := r.Host

	if origin := r.Header.Get("Origin"); origin != "" && origin != "null" {
		u, err := url.Parse(origin)
		return err == nil && strings.EqualFold(u.Host, host)
	}

	if referer := r.Header.Get("Referer"); referer != "" {
		u, err := url.Parse(referer)
		return err == nil && strings.EqualFold(u.Host, host)
	}

	return true
}
