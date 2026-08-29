package layout

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/NorthAIProject/north-client/internal/users"
)

// renderApp is the signed-in shell, which is what every page renders inside.
func renderApp(t *testing.T) string {
	t.Helper()

	var b strings.Builder
	child := templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := w.Write([]byte("<main>page</main>"))
		return err
	})

	page := App("Overview", users.User{DisplayName: "Test"}, BuildNav("/app"))
	if err := page.Render(templ.WithChildren(context.Background(), child), &b); err != nil {
		t.Fatalf("render App: %v", err)
	}
	if b.Len() == 0 {
		// templ flushes nothing on error, so an empty body is the shape a
		// failure deeper in the tree takes even when Render reports success.
		t.Fatal("App rendered an empty document")
	}
	return b.String()
}

// The palette is only useful if it is on every page, which means it has to be
// in the shell rather than opted into per page.
func TestTheShellCarriesThePalette(t *testing.T) {
	t.Parallel()

	html := renderApp(t)

	for _, want := range []string{
		`id="command-palette"`,
		`id="command-palette-list"`,
		`x-data="commandPalette"`,
		`/assets/js/shared/command-palette.js`,
		// templUI resolves its own scripts to a .min.js twin, so match the
		// component rather than a filename this package does not control.
		`/assets/js/dialog`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the app shell does not contain %s", want)
		}
	}
}

// Every destination has to actually be in the list the browser filters. A row
// that fails to render is a page the palette silently cannot reach, and the
// palette is the only way to reach most of them.
func TestEveryDestinationIsInThePalette(t *testing.T) {
	t.Parallel()

	html := renderApp(t)

	for _, d := range Destinations() {
		if !strings.Contains(html, `href="`+d.Href+`"`) {
			t.Errorf("%q (%s) is missing from the rendered palette", d.Label, d.Href)
		}
		if !strings.Contains(html, `data-haystack="`+haystack(d)+`"`) {
			t.Errorf("%q has no haystack attribute, so nothing will ever match it", d.Label)
		}
	}
}

// The haystack is lowercased in Go precisely so the browser never has to, and
// the matcher lowercases the query only. An uppercase character here would
// make that row unmatchable by the text it contains.
func TestTheHaystackIsLowercasedAndCarriesTheKeywords(t *testing.T) {
	t.Parallel()

	for _, d := range Destinations() {
		hay := haystack(d)
		if hay != strings.ToLower(hay) {
			t.Errorf("%q has an uppercase haystack: %q", d.Label, hay)
		}
		if !strings.Contains(hay, strings.ToLower(d.Label)) {
			t.Errorf("%q is not matched by its own label", d.Label)
		}
		for _, k := range d.Keywords {
			if !strings.Contains(hay, k) {
				t.Errorf("%q does not carry its keyword %q", d.Label, k)
			}
		}
	}
}

// The sidebar's own shortcut is Cmd/Ctrl+B (templUI's default). The palette
// claims K. If someone rebinds the sidebar to K the two silently fight, and
// the symptom — a sidebar that toggles when you meant to search — reads as a
// palette bug rather than a configuration one.
func TestTheSidebarDoesNotClaimThePaletteShortcut(t *testing.T) {
	t.Parallel()

	html := renderApp(t)
	if strings.Contains(html, `data-tui-sidebar-keyboard-shortcut="k"`) {
		t.Error("the sidebar is bound to k, which is the palette's shortcut")
	}
}
