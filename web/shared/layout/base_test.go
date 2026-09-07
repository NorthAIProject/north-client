package layout

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/NorthAIProject/north-client/internal/shared/middleware"
)

// Base is the outermost document, so anything wrong in it is wrong on every
// page at once. templ renders into a buffer and flushes nothing on error, which
// means a failure anywhere in the tree arrives as a 200 with an empty body —
// legible as a blank page and nothing else. This renders Base on its own so
// that when that happens, the document itself can be ruled in or out in a
// second rather than by bisecting the application.
func TestBaseRendersTheDocumentShell(t *testing.T) {
	var b strings.Builder
	child := templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := w.Write([]byte("<main>hello</main>"))
		return err
	})

	if err := Base("Settings").Render(templ.WithChildren(context.Background(), child), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := b.String()

	if len(out) == 0 {
		t.Fatal("Base rendered nothing")
	}
	for _, want := range []string{
		"<!doctype html>",
		`<html lang="en"`,
		"<title>Settings · Khepri</title>",
		"<main>hello</main>",
	} {
		if !strings.Contains(strings.ToLower(out), strings.ToLower(want)) {
			t.Errorf("document is missing %q", want)
		}
	}
}

// The favicon is the one piece of chrome with no visible fallback: if the link
// is dropped, the tab shows a blank page icon and nobody files a bug about it.
func TestBaseLinksBothFavicons(t *testing.T) {
	var b strings.Builder
	if err := Base("").Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := b.String()

	// SVG first so a browser that understands it takes the sharp one; the PNG
	// is the tab fallback. Install icons are linked separately as apple-touch-icon.
	svg := strings.Index(out, `href="/assets/brand/favicon.svg"`)
	png := strings.Index(out, `href="/assets/brand/khepri-logo-mark.png"`)

	switch {
	case svg < 0:
		t.Error("no SVG favicon link")
	case png < 0:
		t.Error("no PNG favicon fallback")
	case svg > png:
		t.Error("the PNG fallback is declared before the SVG; browsers take the last they understand")
	}
}

// Install chrome is the one thing a missing tag silently breaks: iOS Add to
// Home Screen and Chromium's install criteria both fail closed, and the
// symptom is "this site is not installable" with no error on the page.
func TestBaseDeclaresPWAChrome(t *testing.T) {
	var b strings.Builder
	if err := Base("").Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := b.String()

	for _, want := range []string{
		"viewport-fit=cover",
		`rel="manifest"`,
		`href="/manifest.webmanifest"`,
		`rel="apple-touch-icon"`,
		`href="/assets/brand/pwa-180.png"`,
		`name="theme-color"`,
		"#1C1C1F",
		`name="apple-mobile-web-app-capable"`,
		`name="mobile-web-app-capable"`,
		"/assets/js/shared/pwa.js",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("document is missing %q", want)
		}
	}
}

// The analytics snippet is the one part of the document that must be able to
// not be there. A deployment with no PostHog key renders nothing, and a
// deployment with one renders its configuration as a JSON island rather than
// as an inline object — because every value in it came from a request.
func TestBaseRendersTheAnalyticsSnippetOnlyWhenConfigured(t *testing.T) {
	render := func(ctx context.Context) string {
		var b strings.Builder
		child := templ.ComponentFunc(func(_ context.Context, _ io.Writer) error { return nil })
		if err := Base("Home").Render(templ.WithChildren(ctx, child), &b); err != nil {
			t.Fatalf("render: %v", err)
		}
		return b.String()
	}

	// Nothing mounted the middleware: no script, and no empty config island
	// for a browser to trip over.
	out := render(context.Background())
	for _, unwanted := range []string{"posthog", "analytics-config"} {
		if strings.Contains(strings.ToLower(out), unwanted) {
			t.Errorf("an unconfigured document mentions %q", unwanted)
		}
	}

	out = render(middleware.WithAnalytics(context.Background(), middleware.AnalyticsConfig{
		APIKey: "phc_test",
		Host:   "https://ph.example",
	}))
	for _, want := range []string{
		`id="analytics-config"`,
		"application/json",
		"/assets/js/vendor/posthog.min.js",
		"/assets/js/shared/analytics.js",
		"phc_test",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("a configured document is missing %q", want)
		}
	}

	// The configuration belongs in the JSON island. An inline init call would
	// mean a request value had been spliced into a script body.
	if strings.Contains(out, "posthog.init(") {
		t.Error("the document inlines a posthog.init call; it belongs in analytics.js")
	}
}
