package i18n

import (
	"net/http"
	"time"

	"golang.org/x/text/language"
)

// CookieName remembers a language chosen before there is an account to store
// it on. Signed-in users are served their account's locale instead — see
// auth.Middleware.LoadUser — so this only ever decides for a visitor.
const CookieName = "locale"

// cookieLifetime outlives a browsing session deliberately. Somebody who
// switched to Portuguese on the landing page and comes back a week later has
// not changed their mind about what language they read.
const cookieLifetime = 365 * 24 * time.Hour

// tags are the locales this build serves, in preference order. The first is
// the fallback the matcher returns when nothing matches, which is why English
// is first rather than in alphabetical company.
//
// A browser that sends a bare "pt" gets Brazilian Portuguese, not European.
// That is x/text resolving the tag through CLDR's likely-subtags data rather
// than through this list's order, and it is right: Brazil has roughly twenty
// times Portugal's Portuguese speakers. users.ResolveLocale answers "pt" the
// same way, deliberately — two paths disagreeing about it would be a bug
// waiting for a browser to send it.
var tags = []language.Tag{
	language.MustParse("en"),
	language.MustParse("pt-PT"),
	language.MustParse("pt-BR"),
	language.MustParse("es"),
}

// names are the catalogue keys, positionally aligned with tags.
var names = []string{"en", "pt-PT", "pt-BR", "es"}

var matcher = language.NewMatcher(tags)

// Negotiate picks the best served locale for an Accept-Language header.
//
// x/text/language rather than a hand-rolled parser: the header carries quality
// values, wildcards, region and script subtags and deprecated tag aliases, and
// getting "pt-BR;q=0.9, pt;q=0.8, en;q=0.7" right by hand is exactly the kind
// of thing that looks finished and is not. The package is already in the module
// graph.
func Negotiate(header string) string {
	if header == "" {
		return DefaultLocale
	}
	preferred, _, err := language.ParseAcceptLanguage(header)
	if err != nil {
		// A malformed header is a browser problem, not a user preference.
		return DefaultLocale
	}
	_, index, confidence := matcher.Match(preferred...)
	if confidence == language.No || index < 0 || index >= len(names) {
		return DefaultLocale
	}
	return names[index]
}

// FromRequest resolves the locale for a request with no signed-in user.
//
// An explicit choice beats a guess: the cookie is what the footer switcher
// writes, and it exists precisely so that a visitor the header got wrong is not
// stuck. Everything else falls back to the header, then to English.
func FromRequest(r *http.Request) string {
	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		if _, served := catalogues[c.Value]; served {
			return c.Value
		}
	}
	return Negotiate(r.Header.Get("Accept-Language"))
}

// SetCookie remembers an explicit choice. An unserved tag clears the cookie
// rather than storing something that will be ignored on every later request.
func SetCookie(w http.ResponseWriter, r *http.Request, tag string) {
	c := &http.Cookie{
		Name:     CookieName,
		Value:    tag,
		Path:     "/",
		Expires:  time.Now().Add(cookieLifetime),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	}
	if _, served := catalogues[tag]; !served {
		c.Value = ""
		c.Expires = time.Unix(0, 0)
		c.MaxAge = -1
	}
	http.SetCookie(w, c)
}

// Served returns the locales this build can render, in the order a picker
// should list them.
func Served() []string {
	out := make([]string, len(names))
	copy(out, names)
	return out
}
