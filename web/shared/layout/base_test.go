package layout

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/a-h/templ"
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
		"<title>Settings · North</title>",
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
	// is the fallback and the source for PWA icon sizes.
	svg := strings.Index(out, `href="/assets/brand/favicon.svg"`)
	png := strings.Index(out, `href="/assets/brand/north-logo-mark.png"`)

	switch {
	case svg < 0:
		t.Error("no SVG favicon link")
	case png < 0:
		t.Error("no PNG favicon fallback")
	case svg > png:
		t.Error("the PNG fallback is declared before the SVG; browsers take the last they understand")
	}
}
