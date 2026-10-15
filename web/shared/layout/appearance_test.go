package layout

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/theme"
)

func renderBase(t *testing.T, a theme.Appearance) string {
	t.Helper()
	var b strings.Builder
	if err := Base("").Render(theme.WithAppearance(context.Background(), a), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

// Each mode is decided in the first bytes of the document, so none of them can
// flash the other on load.
func TestBaseDrawsTheAppearanceIntoTheDocument(t *testing.T) {
	cases := map[theme.Appearance][]string{
		theme.Dark: {
			`<html lang="en" class="h-full dark" data-appearance="dark">`,
			`<meta name="color-scheme" content="dark">`,
			`<meta name="theme-color" content="#1C1C1F">`,
			`content="black-translucent"`,
		},
		theme.Light: {
			`<html lang="en" class="h-full" data-appearance="light">`,
			`<meta name="color-scheme" content="light">`,
			`<meta name="theme-color" content="#FCFDFF">`,
			`name="apple-mobile-web-app-status-bar-style" content="default"`,
		},
		theme.System: {
			`<html lang="en" class="h-full" data-appearance="system">`,
			`<meta name="color-scheme" content="light dark">`,
			`<meta name="theme-color" media="(prefers-color-scheme: light)" content="#FCFDFF">`,
			`<meta name="theme-color" media="(prefers-color-scheme: dark)" content="#1C1C1F">`,
		},
	}
	for a, wants := range cases {
		out := renderBase(t, a)
		for _, want := range wants {
			if !strings.Contains(out, want) {
				t.Errorf("%s: document is missing %s", a, want)
			}
		}
	}
}

// The script that settles "system" has to run before the stylesheet can
// paint, or a dark-OS visitor sees a light frame first.
func TestTheAppearanceScriptRunsBeforeTheStylesheet(t *testing.T) {
	out := renderBase(t, theme.System)
	script := strings.Index(out, `prefers-color-scheme: dark)");`)
	css := strings.Index(out, `/assets/css/output.css`)
	if script < 0 || css < 0 || script > css {
		t.Fatalf("script at %d, stylesheet at %d: the script must come first", script, css)
	}
	if !strings.Contains(out, `"theme-changed"`) {
		t.Error("the script does not announce theme-changed")
	}
}

// A page rendered with no appearance on the context — outside the middleware —
// keeps today's look.
func TestBaseWithoutAnAppearanceUsesTheDefault(t *testing.T) {
	var b strings.Builder
	if err := Base("").Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(b.String(), `data-appearance="`+string(theme.DefaultAppearance)+`"`) {
		t.Fatal("the default appearance was not rendered")
	}
}
