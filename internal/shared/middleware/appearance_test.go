package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/shared/theme"
)

func appearanceOf(r *http.Request) theme.Appearance {
	var got theme.Appearance
	middleware.Appearance(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = theme.AppearanceFrom(r.Context())
	})).ServeHTTP(httptest.NewRecorder(), r)
	return got
}

func TestAVisitorsCookieDecidesTheirAppearance(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.AddCookie(&http.Cookie{Name: theme.CookieName, Value: "light"})

	if got := appearanceOf(r); got != theme.Light {
		t.Fatalf("appearance = %q, want light", got)
	}
}

func TestAVisitorWithNoCookieGetsTheDefault(t *testing.T) {
	if got := appearanceOf(httptest.NewRequest(http.MethodGet, "/", nil)); got != theme.DefaultAppearance {
		t.Fatalf("appearance = %q, want the default", got)
	}
}
