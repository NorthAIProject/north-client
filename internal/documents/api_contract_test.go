package documents

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/documents/document"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestKnowledgeShapes(t *testing.T) {
	t.Parallel()

	indexed := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	doc := DocumentView{
		ID: uuid.MustParse("c9c9c9c9-c9c9-c9c9-c9c9-c9c9c9c9c9c9"), Title: "Marathon plan.md", Kind: "upload",
		MIME: "text/markdown", ByteSize: 4210, Status: "ready", IndexedAt: &indexed, CreatedAt: indexed.Add(-time.Minute),
	}
	apitest.AssertGolden(t, "knowledge.golden.json", KnowledgeList{Documents: []DocumentView{doc}, Counts: KnowledgeCounts{Ready: 1}})
	apitest.AssertGolden(t, "knowledge-document.golden.json", DocumentDetail{DocumentView: doc, Text: "# Marathon plan\nWeek 1: three easy runs."})
	apitest.AssertGolden(t, "knowledge-search.golden.json", SearchResults{Hits: []SearchHit{{
		DocumentID: doc.ID, Title: doc.Title, HeadingPath: []string{"Marathon plan"}, Snippet: "Week 1: three easy runs.",
		Segments: []SnippetSegment{{Text: "Week 1: three "}, {Text: "easy runs", Matched: true}, {Text: "."}}, StartLine: 2,
	}}})
}

// Raw snippets carry ts_headline's control-character markers. A client gets
// plain text and marked segments, never the markers.
func TestSearchHitsResolveHighlightMarkers(t *testing.T) {
	t.Parallel()

	hit := projectHit(Hit{Snippet: document.MarkStart + "Wall" + document.MarkEnd + " " + document.MarkStart + "sits" + document.MarkEnd + ", three sets."})
	if hit.Snippet != "Wall sits, three sets." {
		t.Errorf("snippet = %q", hit.Snippet)
	}
	if len(hit.Segments) == 0 || hit.Segments[0].Text != "Wall" || !hit.Segments[0].Matched {
		t.Errorf("segments = %+v, want Wall marked first", hit.Segments)
	}
	if hit.HeadingPath == nil {
		t.Error("headingPath must be an empty array, not null")
	}
}
