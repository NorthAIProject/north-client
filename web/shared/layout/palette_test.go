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

	page := App("Overview", users.User{DisplayName: "Test"}, BuildNav(context.Background(), "/app"))
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

// The search trigger used to be a fixed width that would not shrink, so on a
// tight header it painted across the notification bell. It has to truncate
// inside its own box, and the bell has to keep its slot.
func TestSearchTriggerYieldsToTheBell(t *testing.T) {
	t.Parallel()

	html := renderApp(t)

	button := classTokens(t, openingTag(t, html, `aria-label="Search pages"`))
	for _, want := range []string{"min-w-0", "max-w-64", "shrink", "overflow-hidden"} {
		if !button[want] {
			t.Errorf("search trigger is missing %s", want)
		}
	}
	for _, banned := range []string{"sm:w-64", "lg:w-80", "shrink-0"} {
		if button[banned] {
			t.Errorf("search trigger still covers the bell with %s", banned)
		}
	}

	bell := openingTag(t, html, `hx-get="/app/nudges/bell"`)
	if !strings.Contains(bell, "shrink-0") {
		t.Errorf("bell container gives up its width: %s", bell)
	}
	cluster := openingTag(t, html, `class="ml-auto flex min-w-0 items-center gap-2"`)
	if cluster == "" {
		t.Error("header actions cannot shrink, so the search still overflows onto the bell")
	}
}

// openingTag is the start tag that contains marker, so a class assertion can
// target one control instead of the whole document.
func openingTag(t *testing.T, html, marker string) string {
	t.Helper()
	i := strings.Index(html, marker)
	if i < 0 {
		t.Fatalf("rendered shell has no %s", marker)
	}
	start := strings.LastIndex(html[:i], "<")
	end := strings.Index(html[i:], ">")
	if start < 0 || end < 0 {
		t.Fatalf("no tag around %s", marker)
	}
	return html[start : i+end+1]
}

// classTokens is the class attribute of a tag, split on whitespace, so a
// check for "shrink" does not also match "[&_svg]:shrink-0".
func classTokens(t *testing.T, tag string) map[string]bool {
	t.Helper()
	const key = `class="`
	i := strings.Index(tag, key)
	if i < 0 {
		t.Fatalf("no class attribute on %s", tag)
	}
	rest := tag[i+len(key):]
	j := strings.Index(rest, `"`)
	if j < 0 {
		t.Fatalf("unclosed class attribute on %s", tag)
	}
	out := map[string]bool{}
	for _, token := range strings.Fields(rest[:j]) {
		out[token] = true
	}
	return out
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
		if !strings.Contains(html, `data-haystack="`+haystack(context.Background(), d)+`"`) {
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
		hay := haystack(context.Background(), d)
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
