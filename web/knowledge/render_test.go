package knowledge

import (
	"context"
	"fmt"
	html2 "html"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/documents/document"
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()

	var b strings.Builder
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

// The source view is where a citation is checked. Every line has to be
// individually addressable, or the anchor in a hit's URL lands nowhere.
func TestSourceViewAnchorsEveryLine(t *testing.T) {
	view := DocumentView{
		Lines: []string{"# Physio notes", "", "Wide grip aggravates it every time.", "Narrow grip is fine."},
	}

	html := render(t, source(view))

	for i := range view.Lines {
		if anchor := fmt.Sprintf(`id="L%d"`, i+1); !strings.Contains(html, anchor) {
			t.Errorf("no anchor %s in the rendered document", anchor)
		}
	}
	for _, line := range view.Lines {
		if line == "" {
			continue
		}
		if !strings.Contains(html, line) {
			t.Errorf("line %q is missing from the rendered document", line)
		}
	}
}

// The highlight has to fall on exactly the cited lines. One row out and the
// page emphasises text the coach never quoted, which is a wrong answer wearing
// evidence.
func TestSourceViewHighlightsOnlyTheCitedLines(t *testing.T) {
	view := DocumentView{
		Lines:     []string{"one", "two", "three", "four", "five"},
		FromLine:  2,
		ToLine:    3,
		Highlight: true,
	}

	for line := 1; line <= len(view.Lines); line++ {
		want := line >= view.FromLine && line <= view.ToLine
		if got := view.isHighlighted(line); got != want {
			t.Errorf("isHighlighted(%d) = %v, want %v", line, got, want)
		}
	}

	html := render(t, source(view))

	// Count the highlighted rows in the markup itself, so a change to how the
	// class is applied cannot quietly stop highlighting anything.
	if got, want := strings.Count(html, "bg-signal/12"), 2; got != want {
		t.Errorf("%d highlighted rows in the markup, want %d", got, want)
	}

	// A document opened without a citation highlights nothing.
	plain := render(t, source(DocumentView{Lines: view.Lines}))
	if strings.Contains(plain, "bg-signal/12") {
		t.Error("a document opened with no passage still highlights a row")
	}
}

// A result row that could not be opened would be an assertion rather than a
// citation.
func TestResultRowLinksToThePassage(t *testing.T) {
	hit := document.Hit{
		ChunkID:     "nor_chk_abc",
		DocumentID:  uuid.New(),
		Title:       "Physio notes",
		HeadingPath: []string{"Physio notes", "Overhead pressing"},
		StartLine:   40,
		EndLine:     58,
		Snippet:     "narrow grip and " + document.MarkStart + "landmine" + document.MarkEnd + " pressing",
	}

	html := render(t, resultRow(hit))

	// Compared against the escaped form: the href carries two query
	// parameters, so a correctly rendered page writes &amp; between them.
	if want := html2.EscapeString(hit.URL()); !strings.Contains(html, want) {
		t.Errorf("the row does not link to %s\n%s", want, html)
	}
	if !strings.Contains(html, "L40–58") {
		t.Error("the row does not name the line range")
	}
	if !strings.Contains(html, "Overhead pressing") {
		t.Error("the row does not name the heading the passage sits under")
	}

	// The title is dropped from the trail; repeating it reads as a stutter.
	if strings.Contains(html, "› Physio notes") {
		t.Error("the trail repeats the document title")
	}

	if !strings.Contains(html, "<mark") || !strings.Contains(html, "landmine") {
		t.Errorf("the matched term is not emphasised:\n%s", html)
	}

	// The markers are a transport detail and must never reach the page.
	if strings.Contains(html, document.MarkStart) || strings.Contains(html, document.MarkEnd) {
		t.Error("a snippet marker leaked into the rendered HTML")
	}
}

// The passage is the person's own writing and is never trusted markup.
func TestResultRowEscapesThePassage(t *testing.T) {
	hit := document.Hit{
		DocumentID: uuid.New(),
		Title:      "Notes",
		StartLine:  1,
		EndLine:    1,
		Snippet:    "before " + document.MarkStart + "<script>alert(1)</script>" + document.MarkEnd + " after",
	}

	html := render(t, resultRow(hit))

	if strings.Contains(html, "<script>") {
		t.Errorf("a script tag from the document reached the page unescaped:\n%s", html)
	}
	if !strings.Contains(html, "&lt;script&gt;") {
		t.Errorf("the passage was not escaped as expected:\n%s", html)
	}
}

func TestPassagesSaysSoWhenNothingResolves(t *testing.T) {
	html := render(t, Passages(nil))

	if !strings.Contains(html, "no longer in your knowledge") {
		t.Errorf("an empty sources list says nothing to the reader:\n%s", html)
	}
}
