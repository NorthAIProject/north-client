package markdown

import (
	"strings"
	"testing"
)

func TestRendersCommonMarkdown(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"**bold**":                   "<strong>bold</strong>",
		"- one\n- two":               "<li>one</li>",
		"1. first\n2. second":        "<ol>",
		"`squat`":                    "<code>squat</code>",
		"## Session one":             "<h2",
		"[link](https://north.test)": `href="https://north.test"`,
	}

	for source, want := range tests {
		t.Run(source, func(t *testing.T) {
			t.Parallel()

			if got := RenderString(source); !strings.Contains(got, want) {
				t.Errorf("rendering %q produced %q, expected it to contain %q", source, got, want)
			}
		})
	}
}

// Model output is untrusted: it reflects whatever the user typed and whatever
// their uploaded documents contain. Raw HTML must be escaped, not passed
// through, or a coaching reply becomes a script injection.
func TestRawHTMLIsEscaped(t *testing.T) {
	t.Parallel()

	dangerous := []string{
		`<script>alert(1)</script>`,
		`<img src=x onerror="alert(1)">`,
		`<iframe src="https://evil.example.com"></iframe>`,
		`<a href="javascript:alert(1)">click</a>`,
	}

	for _, source := range dangerous {
		t.Run(source, func(t *testing.T) {
			t.Parallel()

			got := RenderString(source)

			for _, forbidden := range []string{"<script", "<iframe", "onerror=", "javascript:"} {
				if strings.Contains(strings.ToLower(got), forbidden) {
					t.Errorf("rendering %q leaked %q into the output:\n%s", source, forbidden, got)
				}
			}
		})
	}
}

// The same applies to HTML smuggled inside markdown constructs, where it is
// easy to assume the surrounding syntax makes it safe.
func TestHTMLInsideMarkdownIsAlsoEscaped(t *testing.T) {
	t.Parallel()

	got := RenderString("Here is a tip: <script>alert(1)</script>\n\n- and <b onclick=\"steal()\">a list item</b>")

	if strings.Contains(strings.ToLower(got), "<script") {
		t.Errorf("inline HTML was not escaped:\n%s", got)
	}
	if strings.Contains(strings.ToLower(got), "onclick=") {
		t.Errorf("an event handler survived:\n%s", got)
	}
}

func TestPlainTextSurvivesUnchanged(t *testing.T) {
	t.Parallel()

	// The common case: a coach reply with no markdown at all must read
	// normally rather than being mangled.
	got := RenderString("Add 2.5kg next session and see how the last set feels.")

	if !strings.Contains(got, "Add 2.5kg next session") {
		t.Fatalf("plain prose was altered: %s", got)
	}
}

func TestEmptyInput(t *testing.T) {
	t.Parallel()

	if got := strings.TrimSpace(RenderString("")); got != "" {
		t.Fatalf("empty input produced %q", got)
	}
}
