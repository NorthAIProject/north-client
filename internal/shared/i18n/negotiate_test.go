package i18n

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Accept-Language is not a tag, it is a weighted list. These are the shapes
// real browsers send.
func TestNegotiate(t *testing.T) {
	cases := map[string]string{
		// Exact matches.
		"en":    "en",
		"pt-PT": "pt-PT",
		"pt-BR": "pt-BR",
		"es":    "es",

		// A bare "pt" resolves through CLDR's likely-subtags data, not through
		// the order of the supported list: Brazil has roughly twenty times
		// Portugal's Portuguese speakers. users.ResolveLocale agrees.
		"pt": "pt-BR",

		// Quality values decide, not order in the string.
		"en;q=0.5, pt-BR;q=0.9":     "pt-BR",
		"pt-BR;q=0.2, en;q=0.8":     "en",
		"de;q=1.0, es;q=0.4":        "es",
		"fr-CA, fr;q=0.9, en;q=0.8": "en",

		// Regions this build does not carry fall back within the language.
		"pt-AO":  "pt-PT",
		"es-MX":  "es",
		"es-419": "es",
		"en-GB":  "en",
		"en-US":  "en",

		// Nothing served, and nothing at all.
		"ja, ko;q=0.9": "en",
		"":             "en",

		// A malformed header is a browser problem, not a preference.
		"!!!":             "en",
		"en;q=notanumber": "en",
	}

	for header, want := range cases {
		if got := Negotiate(header); got != want {
			t.Errorf("Negotiate(%q) = %q, want %q", header, got, want)
		}
	}
}

// Every tag Negotiate can return must have a catalogue, or negotiation would
// silently serve English while claiming otherwise.
func TestNegotiateOnlyReturnsServedLocales(t *testing.T) {
	for _, tag := range Served() {
		if _, ok := catalogues[tag]; !ok {
			t.Errorf("Served() offers %q, which has no catalogue", tag)
		}
	}
	if len(Served()) != len(catalogues) {
		t.Errorf("Served() lists %d locales, %d catalogues exist", len(Served()), len(catalogues))
	}
}

// An explicit choice has to beat a guess, or the switcher is decoration.
func TestTheCookieBeatsTheHeader(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Language", "pt-BR")
	r.AddCookie(&http.Cookie{Name: CookieName, Value: "es"})

	if got := FromRequest(r); got != "es" {
		t.Errorf("FromRequest = %q, want the cookie's %q", got, "es")
	}
}

// A cookie holding a locale this build has retired must not win, or a user
// would be stuck reading English with no way to see why.
func TestAnUnservedCookieIsIgnored(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Accept-Language", "pt-BR")
	r.AddCookie(&http.Cookie{Name: CookieName, Value: "kl-GL"})

	if got := FromRequest(r); got != "pt-BR" {
		t.Errorf("FromRequest = %q, want the header's %q", got, "pt-BR")
	}
}

func TestSetCookieStoresAServedLocale(t *testing.T) {
	rec := httptest.NewRecorder()
	SetCookie(rec, httptest.NewRequest(http.MethodPost, "/locale", nil), "pt-BR")

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one cookie, got %d", len(cookies))
	}
	c := cookies[0]
	if c.Name != CookieName || c.Value != "pt-BR" {
		t.Errorf("cookie = %s=%s", c.Name, c.Value)
	}
	if !c.HttpOnly {
		t.Error("the locale cookie is readable by scripts")
	}
	if c.SameSite != http.SameSiteLaxMode {
		t.Error("the locale cookie is not Lax")
	}
}

// An unserved tag clears the cookie rather than storing something every later
// request will ignore.
func TestSetCookieClearsForAnUnservedLocale(t *testing.T) {
	rec := httptest.NewRecorder()
	SetCookie(rec, httptest.NewRequest(http.MethodPost, "/locale", nil), "kl-GL")

	c := rec.Result().Cookies()[0]
	if c.Value != "" || c.MaxAge >= 0 {
		t.Errorf("cookie was not cleared: value=%q maxage=%d", c.Value, c.MaxAge)
	}
}

// Secure is set from the request rather than hardcoded: a Secure cookie on
// plain http is dropped, which would break the switcher in local development
// and leave no trace of why.
func TestTheCookieIsSecureOnlyOverTLS(t *testing.T) {
	plain := httptest.NewRecorder()
	SetCookie(plain, httptest.NewRequest(http.MethodPost, "http://localhost/locale", nil), "es")
	if plain.Result().Cookies()[0].Secure {
		t.Error("the cookie is Secure over plain http, so the browser will drop it")
	}
}
