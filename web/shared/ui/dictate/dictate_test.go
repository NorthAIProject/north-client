package dictate

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/voice"
)

func render(t *testing.T, ctx context.Context, c templ.Component) string {
	t.Helper()

	var b strings.Builder
	if err := c.Render(ctx, &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

func box() templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, `<textarea id="wins"></textarea>`)
		return err
	})
}

// field renders Field around the textarea, the way a page's children block
// would supply it.
func field(props Props) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		return Field(props).Render(templ.WithChildren(ctx, box()), w)
	})
}

func withDictation(available bool) context.Context {
	return i18n.WithLocale(voice.WithDictation(context.Background(), available), i18n.DefaultLocale)
}

// The button names its box, carries its limits and its copy, and starts hidden
// so a browser that cannot record never sees it.
func TestFieldWrapsTheBoxWithAMicrophone(t *testing.T) {
	html := render(t, withDictation(true), field(Props{Target: "wins"}))

	for _, want := range []string{
		`<textarea id="wins">`,
		`data-dictate-target="wins"`,
		`aria-controls="wins"`,
		`data-dictate-max-seconds="120"`,
		`data-dictate-say="Say it"`,
		" hidden",
		"data-dictate-error",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in %s", want, html)
		}
	}

	// Dictation is recorded as dictation unless a page says otherwise, and the
	// only way to say otherwise is the allowlist on the server.
	if strings.Contains(html, "data-dictate-surface") {
		t.Error("a plain field names a surface")
	}
}

// With nothing to listen with, the box renders exactly as it did before.
func TestFieldIsJustTheBoxWhereNothingCanListen(t *testing.T) {
	html := render(t, withDictation(false), field(Props{Target: "wins"}))

	if html != `<textarea id="wins"></textarea>` {
		t.Fatalf("html = %q, want the textarea alone", html)
	}
	if got := render(t, withDictation(false), Script()); got != "" {
		t.Fatalf("script rendered with dictation off: %q", got)
	}
}

func TestScriptRendersOncePerPage(t *testing.T) {
	// One page is one render context; the handle is scoped to it.
	ctx := templ.InitializeContext(withDictation(true))
	var b strings.Builder
	for range 3 {
		if err := Script().Render(ctx, &b); err != nil {
			t.Fatalf("render: %v", err)
		}
	}
	if n := strings.Count(b.String(), "dictate.js"); n != 1 {
		t.Fatalf("script tag rendered %d times, want 1", n)
	}
}
