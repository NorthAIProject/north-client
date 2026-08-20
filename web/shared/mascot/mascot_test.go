package mascot

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func render(t *testing.T, p Props) string {
	t.Helper()

	var b strings.Builder
	if err := Mascot(p).Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

// The canvas is the component. Everything else — the Alpine binding, the
// fallback — hangs off it, so this is the assertion that fails loudest if the
// template is ever restructured.
func TestMascotRendersACanvasBoundToAlpine(t *testing.T) {
	out := render(t, Props{ID: "onboarding", Size: SizeLg, State: StateListening})

	for _, want := range []string{
		`x-ref="canvas"`,
		`id="onboarding"`,
		"northMascot(",
		`&#34;state&#34;:&#34;listening&#34;`,
		`&#34;id&#34;:&#34;onboarding&#34;`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// A gesture unwinds the moment it plays, so starting in one would mean
// starting in a pose that immediately undoes itself. Anything that is not a
// sustained state has to come back as idle.
func TestMascotFallsBackToIdleForAnUnsupportedState(t *testing.T) {
	for _, given := range []State{"celebrate", "nod", "", "nonsense"} {
		out := render(t, Props{State: given})
		if !strings.Contains(out, `&#34;state&#34;:&#34;idle&#34;`) {
			t.Errorf("state %q did not fall back to idle:\n%s", given, out)
		}
	}
}

// Without WebGL the component still has to say something on the page, and the
// brand PNG is what it says. If this asset path drifts, the no-WebGL path
// silently renders a broken image.
func TestMascotFallsBackToTheBrandImage(t *testing.T) {
	out := render(t, Props{})

	if !strings.Contains(out, `src="/assets/brand/north-mascot.png"`) {
		t.Errorf("fallback image missing:\n%s", out)
	}
	if !strings.Contains(out, `x-show="failed"`) {
		t.Errorf("fallback is not gated on the failed state:\n%s", out)
	}
	// The mascot sits beside copy that already names the product on every
	// surface it appears on; announcing it again is noise, not access.
	if !strings.Contains(out, `alt=""`) {
		t.Errorf("fallback should be decorative:\n%s", out)
	}
}

func TestMascotSizesMapToDistinctBoxes(t *testing.T) {
	seen := map[string]Size{}
	for _, size := range []Size{SizeSm, SizeMd, SizeLg} {
		class := boxClass(size)
		if previous, clash := seen[class]; clash {
			t.Fatalf("sizes %q and %q render the same box %q", previous, size, class)
		}
		seen[class] = size

		if !strings.Contains(render(t, Props{Size: size}), class) {
			t.Errorf("size %q did not apply %q", size, class)
		}
	}

	// An unset Size is the common case at a call site that does not care, and
	// it has to land on something rather than on an empty class.
	if boxClass("") != boxClass(SizeMd) {
		t.Errorf("unset size should render as medium, got %q", boxClass(""))
	}
}

// Two mascots on one page must not load the module twice; the OnceHandle is
// what guarantees it, and a refactor that drops it would be invisible without
// this.
func TestScriptEmitsOnlyOncePerRequest(t *testing.T) {
	var b strings.Builder
	// The handle keys its "already emitted" flag off the context, which a
	// request carries for its whole render. Rendering twice against a bare
	// Background would prove nothing.
	ctx := templ.InitializeContext(context.Background())

	for range 2 {
		if err := Script().Render(ctx, &b); err != nil {
			t.Fatalf("render: %v", err)
		}
	}

	if got := strings.Count(b.String(), "<script"); got != 1 {
		t.Errorf("script rendered %d times, want 1", got)
	}
}
