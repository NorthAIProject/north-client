package theme

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The resolution order is the feature: the account beats the cookie, the
// cookie beats the default, and an account that never chose keeps the cookie.
func TestEffectivePrefersTheAccountThenTheCookieThenTheDefault(t *testing.T) {
	cases := []struct {
		name           string
		stored, cookie Appearance
		want           Appearance
	}{
		{"account wins over cookie", Light, Dark, Light},
		{"account system wins", System, Light, System},
		{"never chosen keeps the cookie", "", Light, Light},
		{"nothing at all is the default", "", "", DefaultAppearance},
		{"junk stored reads as never chosen", "sepia", System, System},
		{"junk cookie reads as no cookie", "", "sepia", DefaultAppearance},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Effective(tc.stored, tc.cookie); got != tc.want {
				t.Fatalf("Effective(%q, %q) = %q, want %q", tc.stored, tc.cookie, got, tc.want)
			}
		})
	}
}

// The web stays dark until light mode is signed off. Flipping this is a
// deliberate one-line change, and this test is the place that says so.
func TestTheWebDefaultIsStillDark(t *testing.T) {
	if DefaultAppearance != Dark {
		t.Fatalf("DefaultAppearance = %q; flipping it is the post-audit change, update this test with it", DefaultAppearance)
	}
}

func TestParseForgivesCaseAndSpaceButNothingElse(t *testing.T) {
	for in, want := range map[string]Appearance{" Light ": Light, "DARK": Dark, "system": System} {
		if got, ok := Parse(in); !ok || got != want {
			t.Errorf("Parse(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "auto", "sepia", "lite"} {
		if _, ok := Parse(in); ok {
			t.Errorf("Parse(%q) accepted a value that is not a choice", in)
		}
	}
}

func TestTheCookieRoundTripsWithTheLocaleCookiesAttributes(t *testing.T) {
	rec := httptest.NewRecorder()
	SetCookie(rec, httptest.NewRequest(http.MethodPost, "/appearance", nil), Light)

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("set %d cookies, want 1", len(cookies))
	}
	c := cookies[0]
	if c.Name != "appearance" || c.Value != "light" || c.Path != "/" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie = %+v", c)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(c)
	if got := FromCookie(req); got != Light {
		t.Fatalf("FromCookie = %q, want light", got)
	}
}

func TestAnInvalidChoiceClearsTheCookie(t *testing.T) {
	rec := httptest.NewRecorder()
	SetCookie(rec, httptest.NewRequest(http.MethodPost, "/appearance", nil), "sepia")
	c := rec.Result().Cookies()[0]
	if c.Value != "" || c.MaxAge >= 0 {
		t.Fatalf("cookie = %+v, want it cleared", c)
	}
}

// A page rendered outside the middleware still gets an answer.
func TestAppearanceFromAnEmptyContextIsTheDefault(t *testing.T) {
	if got := AppearanceFrom(context.Background()); got != DefaultAppearance {
		t.Fatalf("AppearanceFrom(empty) = %q, want the default", got)
	}
	if got := AppearanceFrom(WithAppearance(context.Background(), System)); got != System {
		t.Fatalf("AppearanceFrom = %q, want system", got)
	}
}

func TestTheCookieIsSecureBehindATLSTerminatingIngress(t *testing.T) {
	for _, tc := range []struct {
		name   string
		proto  string
		secure bool
	}{
		{"plain http", "", false},
		{"https at the ingress", "https", true},
		{"http at the ingress", "http", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/appearance", nil)
			if tc.proto != "" {
				r.Header.Set("X-Forwarded-Proto", tc.proto)
			}
			w := httptest.NewRecorder()
			SetCookie(w, r, Light)
			cookies := w.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("got %d cookies, want 1", len(cookies))
			}
			if cookies[0].Secure != tc.secure {
				t.Errorf("Secure = %v, want %v", cookies[0].Secure, tc.secure)
			}
		})
	}
}
