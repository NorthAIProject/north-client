package icon

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

var pathData = regexp.MustCompile(` d="([^"]*)"`)

// paths pulls every path's d attribute out of a rendered component, in order.
func paths(t *testing.T, name string) []string {
	t.Helper()

	var b strings.Builder
	if err := BrandFor(name).Render(context.Background(), &b); err != nil {
		t.Fatalf("render %s: %v", name, err)
	}

	matches := pathData.FindAllStringSubmatch(b.String(), -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

// TestVendoredMarksMatchTheirSource pins the transcription.
//
// These marks were copied by hand out of vendor SVGs. A path truncated in the
// middle still renders — as a plausible-looking wrong shape — so "it looks fine
// in the browser" is not evidence that the copy was complete. Each expectation
// below is the first and last 48 characters of the source path plus its exact
// length, which together catch a truncation, a dropped segment, or a mangled
// number without pasting 19KB into a test file.
//
// If one of these fails after somebody re-vendors a mark, re-derive the numbers
// from the new SVG rather than deleting the case.
func TestVendoredMarksMatchTheirSource(t *testing.T) {
	tests := []struct {
		name string

		// lengths is every path's length, in order, measured off the vendor
		// SVG with `re.findall(r' d="([^"]*)"')`.
		lengths    []int
		head, tail string
	}{
		{
			name: "openrouter", lengths: []int{176},
			head: "M18.654 3.87a5.087 5.087 0 110 10.174L23.7 19.09",
			tail: "0 10.176 5.087 5.087 0 000-10.175z",
		},
		{
			name: "anthropic", lengths: []int{163},
			head: "M13.827 3.52h3.603L24 20h-3.603l-6.57-16.48zm-7.",
			tail: ".959L8.453 7.687 6.205 13.48H10.7z",
		},
		{
			name: "claude_code", lengths: []int{1499},
			head: "M4.709 15.955l4.72-2.647.08-.23-.08-.128H9.2l-.7",
			tail: "746.231-.243 1.908-1.312-.006.006z",
		},
		{
			name: "openai", lengths: []int{1485},
			head: "M9.205 8.658v-2.26c0-.19.072-.333.238-.428l4.543",
			tail: "88-.309a5.96 5.96 0 004.162 1.713z",
		},
		{
			// The one most worth pinning: three paths, the last 17.8KB.
			name: "hermes", lengths: []int{1626, 93, 17803},
			head: "M5.938 12.835c.127-.039.285.02.373.143.028.038.0",
			tail: "207c-.025-.03-.053-.059-.097-.108z",
		},
		{
			name: "nvidia", lengths: []int{828},
			head: "M10.212 8.976V7.62c.127-.01.256-.017.388-.021 3.",
			tail: " 16 4.25 10.958 4.25 10.958h-.002z",
		},
		{
			name: "xai", lengths: []int{556},
			head: "M9.27 15.29l7.978-5.897c.391-.29.95-.177 1.137.2",
			tail: "1.199 1.259-1.682 1.925l7.62-6.815",
		},
		{
			// One shape drawn four times: a solid base under three gradient
			// washes. All four paths are byte-identical in the source.
			name: "gemini", lengths: []int{449, 449, 449, 449},
			head: "M20.616 10.835a14.147 14.147 0 01-4.45-3.001 14.",
			tail: "975 13.245 13.245 0 01-2.003-.678z",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := paths(t, tc.name)

			if len(got) != len(tc.lengths) {
				t.Fatalf("path count = %d, want %d", len(got), len(tc.lengths))
			}
			for i, want := range tc.lengths {
				if n := len(got[i]); n != want {
					t.Errorf("path %d length = %d, want %d — the copy lost or gained characters", i, n, want)
				}
			}
			if !strings.HasPrefix(got[0], tc.head) {
				t.Errorf("first path does not start with the source's opening:\n got %.48q", got[0])
			}
			// Checked against the *last* path, which for hermes is the big one
			// — exactly where a truncated paste would show.
			last := got[len(got)-1]
			if !strings.HasSuffix(last, tc.tail) {
				t.Errorf("last path does not end with the source's closing:\nwant …%q\n got …%q",
					tc.tail, last[max(0, len(last)-len(tc.tail)):])
			}
		})
	}
}

// A mark rendered with no fill at all inherits nothing useful and disappears on
// one of the two themes. Each is either currentColor or an explicit brand hex.
func TestEveryMarkDeclaresItsColour(t *testing.T) {
	for _, name := range []string{"openrouter", "anthropic", "claude_code", "openai", "hermes", "gemini", "nvidia", "xai"} {
		var b strings.Builder
		if err := BrandFor(name).Render(context.Background(), &b); err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		out := b.String()
		if !strings.Contains(out, "currentColor") && !strings.Contains(out, "fill=\"#") {
			t.Errorf("%s declares no fill; it will vanish on one theme", name)
		}
	}
}

// Gemini's gradients are referenced by id, and ids in SVG are document-global.
// The source shipped fixed ones, so two Geminis on a page would define the same
// ids twice and every url(#…) would resolve to whichever the browser saw first
// — one icon silently restyling the other. This pins the namespacing that fixes
// it, and would fail the moment somebody re-vendors the mark from source
// without re-applying it.
func TestGeminiGradientIDsAreUniquePerRender(t *testing.T) {
	render := func() string {
		var b strings.Builder
		if err := BrandGemini().Render(context.Background(), &b); err != nil {
			t.Fatalf("render: %v", err)
		}
		return b.String()
	}

	first, second := render(), render()

	idAttr := regexp.MustCompile(`<linearGradient[^>]* id="([^"]+)"`)
	firstIDs := idAttr.FindAllStringSubmatch(first, -1)
	secondIDs := idAttr.FindAllStringSubmatch(second, -1)

	if len(firstIDs) != 3 {
		t.Fatalf("gradient count = %d, want 3", len(firstIDs))
	}

	seen := map[string]bool{}
	for _, m := range firstIDs {
		if seen[m[1]] {
			t.Errorf("gradient id %q is repeated within a single render", m[1])
		}
		seen[m[1]] = true
	}
	for _, m := range secondIDs {
		if seen[m[1]] {
			t.Errorf("gradient id %q reused across two renders; a second Gemini on the page would restyle the first", m[1])
		}
	}

	// Every id defined must also be referenced, or the wash it carries is
	// simply not drawn and the mark loses a colour without anything erroring.
	for _, m := range firstIDs {
		if !strings.Contains(first, "url(#"+m[1]+")") {
			t.Errorf("gradient %q is defined but never referenced", m[1])
		}
	}
}

// An unknown provider must still render something rather than nothing, so a
// catalogue entry added before its mark exists does not leave a hole.
func TestUnknownNameFallsBackToThePlug(t *testing.T) {
	var b strings.Builder
	if err := BrandFor("some-provider-added-tomorrow").Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(b.String(), "<svg") {
		t.Error("unknown provider rendered no icon at all")
	}
}
