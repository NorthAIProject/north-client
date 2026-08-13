package document_test

import (
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/documents/document"
)

// The URL is the whole feature: it is what turns "this came from your physio
// notes" into something a person can open and disagree with.
func TestHitURLCarriesTheLineRange(t *testing.T) {
	id := uuid.New()
	hit := document.Hit{DocumentID: id, StartLine: 40, EndLine: 58}

	parsed, err := url.Parse(hit.URL())
	if err != nil {
		t.Fatalf("Hit.URL() is not a URL: %v", err)
	}

	if want := "/app/knowledge/" + id.String(); parsed.Path != want {
		t.Errorf("path = %q, want %q", parsed.Path, want)
	}
	if got := parsed.Query().Get("from"); got != "40" {
		t.Errorf("from = %q, want 40", got)
	}
	if got := parsed.Query().Get("to"); got != "58" {
		t.Errorf("to = %q, want 58", got)
	}

	// The fragment has to be the id the source view puts on the row, or the
	// browser lands at the top of a document and the reader has to hunt.
	if parsed.Fragment != "L40" {
		t.Errorf("fragment = %q, want L40", parsed.Fragment)
	}
	if n, err := strconv.Atoi(strings.TrimPrefix(parsed.Fragment, "L")); err != nil || n != hit.StartLine {
		t.Errorf("fragment %q does not name the first line of the passage", parsed.Fragment)
	}
}

func TestHitLinesReadsAsACitation(t *testing.T) {
	multi := document.Hit{StartLine: 40, EndLine: 58}
	if got := multi.Lines(); got != "L40–58" {
		t.Errorf("Lines() = %q, want L40–58", got)
	}

	// A one-line passage written as "L12–12" reads like a mistake.
	single := document.Hit{StartLine: 12, EndLine: 12}
	if got := single.Lines(); got != "L12" {
		t.Errorf("Lines() = %q, want L12", got)
	}
}

func TestSegmentsSplitsAMarkedSnippet(t *testing.T) {
	hit := document.Hit{
		Snippet: "narrow grip and " + document.MarkStart + "landmine" + document.MarkEnd + " pressing are fine",
	}

	got := hit.Segments()
	want := []document.Segment{
		{Text: "narrow grip and "},
		{Text: "landmine", Matched: true},
		{Text: " pressing are fine"},
	}

	if len(got) != len(want) {
		t.Fatalf("got %d segments, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("segment %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}

// A markdown link in the passage is the case brackets could not survive. The
// control-character markers make the split unambiguous, and this is the test
// that says so.
func TestSegmentsLeaveBracketsAlone(t *testing.T) {
	hit := document.Hit{
		Snippet: "see [the paper](https://example.com) on " + document.MarkStart + "deloads" + document.MarkEnd,
	}

	var marked []string
	for _, s := range hit.Segments() {
		if s.Matched {
			marked = append(marked, s.Text)
		}
	}

	if len(marked) != 1 || marked[0] != "deloads" {
		t.Errorf("marked %#v, want exactly [deloads]", marked)
	}
}

// A snippet cut mid-match should lose its emphasis, not swallow the rest of the
// excerpt into a highlight that runs to the end.
func TestSegmentsTreatAnUnbalancedMarkerAsText(t *testing.T) {
	hit := document.Hit{Snippet: "band pull-aparts " + document.MarkStart + "daily"}

	got := hit.Segments()
	if len(got) != 1 || got[0].Matched {
		t.Fatalf("got %#v, want one unmatched segment", got)
	}
	if !strings.Contains(got[0].Text, "daily") {
		t.Errorf("the text after the marker was dropped: %q", got[0].Text)
	}
}

func TestSegmentsOfAnEmptySnippet(t *testing.T) {
	if got := (document.Hit{}).Segments(); len(got) != 0 {
		t.Errorf("an empty snippet produced %#v", got)
	}
}
