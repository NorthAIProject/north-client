package fitness

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/fitness/strava"
	"github.com/NorthAIProject/north-client/web/assets"
)

// The sport colours have three consumers — the Go family table, the CSS
// tokens, and the palette the WebGL scene reads — and for a long time they had
// no test at all. The hex values lived in the viewer, the swatches lived in
// the legend as Tailwind classes, and a comment asked whoever touched one to
// remember the other. This is that comment, enforced.
//
// The files are read through the embedded asset tree rather than by relative
// path, so the test does not quietly start passing if the repository is
// rearranged and the path stops resolving.

func readAsset(t *testing.T, path string) string {
	t.Helper()
	b, err := assets.Assets.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestEveryFamilyHasAColourTokenInBothThemes(t *testing.T) {
	t.Parallel()

	css := readAsset(t, "css/input.css")

	// The light values live on :root and the dark ones under .dark. Counting
	// occurrences rather than locating the blocks: a token defined once is a
	// token that only works in one theme, which is exactly the bug where a
	// terrain goes invisible on a white background.
	for _, f := range strava.Families {
		token := fmt.Sprintf("--north-sport-%s:", f)
		if n := strings.Count(css, token); n != 2 {
			t.Errorf("%s is defined %d times in input.css, want 2 (light and dark)", token, n)
		}

		utility := fmt.Sprintf("--color-sport-%s: var(--north-sport-%s);", f, f)
		if !strings.Contains(css, utility) {
			t.Errorf("input.css does not register %s in the @theme block, so bg-sport-%s will not exist", utility, f)
		}
	}
}

func TestEveryFamilyHasAColourInTheScenePalette(t *testing.T) {
	t.Parallel()

	palette := readAsset(t, "js/shared/strava-activities/palette.js")

	for _, f := range strava.Families {
		if !strings.Contains(palette, fmt.Sprintf("  %s: 0x", f)) {
			t.Errorf("palette.js has no fallback for family %q", f)
		}
	}
}

func TestLegendShowsEveryFamilyExactlyOnce(t *testing.T) {
	t.Parallel()

	var buf strings.Builder
	if err := legend().Render(context.Background(), &buf); err != nil {
		t.Fatalf("render legend: %v", err)
	}
	html := buf.String()

	for _, f := range strava.Families {
		if n := strings.Count(html, f.Label()); n != 1 {
			t.Errorf("legend names %q %d times, want 1", f.Label(), n)
		}
		swatch := fmt.Sprintf("bg-sport-%s", f)
		if n := strings.Count(html, swatch); n != 1 {
			t.Errorf("legend uses %q %d times, want 1", swatch, n)
		}
	}
}

// Tailwind finds a utility by scanning source text for the literal class
// string. A class assembled at runtime — "bg-sport-" + family — compiles, and
// renders with no colour at all, with no build error and nothing to grep for.
func TestSwatchClassesAreWrittenOutNotBuilt(t *testing.T) {
	t.Parallel()

	for _, f := range strava.Families {
		want := fmt.Sprintf("bg-sport-%s", f)
		if got := sportSwatch(f); got != want {
			t.Errorf("sportSwatch(%q) = %q, want %q", f, got, want)
		}
	}
}
