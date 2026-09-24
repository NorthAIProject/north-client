package documents

import (
	"testing"
	"time"

	"github.com/google/uuid"

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
		DocumentID: doc.ID, Title: doc.Title, HeadingPath: []string{"Marathon plan"}, Snippet: "Week 1: three easy runs.", StartLine: 2,
	}}})
}
