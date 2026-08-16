package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func renderEmpty(t *testing.T, c templ.Component) string {
	t.Helper()
	var b strings.Builder
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

func TestEmptyRendersTitleBodyAndChildCTA(t *testing.T) {
	html := renderEmpty(t, Empty(EmptyProps{
		Eyebrow: "Start here",
		Title:   "Name one thing you are working toward",
		Body:    "North reads your goals before every reply. One is enough.",
	}))
	for _, want := range []string{
		"Start here",
		"Name one thing you are working toward",
		"North reads your goals before every reply. One is enough.",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestEmptyRendersChildCTA(t *testing.T) {
	ctx := templ.WithChildren(context.Background(), templ.Raw("<button>Add a goal</button>"))
	var b strings.Builder
	if err := Empty(EmptyProps{Title: "Nothing to aim at yet"}).Render(ctx, &b); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "Add a goal") {
		t.Fatal("empty state dropped its child CTA")
	}
}
