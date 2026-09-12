package insights

import (
	"context"
	"strings"
	"testing"

	insightpages "github.com/NorthAIProject/north-client/web/insights"
)

// The panels are what the range selector swaps in, so this is the markup most
// likely to be rendered on its own. An icon name the vendored set does not
// carry makes Icon error, templ flushes nothing, and the page 200s empty —
// which no type check catches.
func TestSummaryPanelsRender(t *testing.T) {
	view, err := buildSummaryView(wellLogged(t))
	if err != nil {
		t.Fatalf("buildSummaryView: %v", err)
	}

	var out strings.Builder
	if err := insightpages.SummaryPanels(view).Render(context.Background(), &out); err != nil {
		t.Fatalf("render: %v", err)
	}

	html := out.String()
	for _, want := range []string{"Scores", "Pinned", "Body", "Mind", "Progress", "Training", "on track"} {
		if !strings.Contains(html, want) {
			t.Errorf("rendered panels do not mention %q", want)
		}
	}
	if !strings.Contains(html, "data-echarts") {
		t.Error("no chart mount rendered, so no ring is drawn")
	}
}

func TestSummaryPanelsOfAnEmptyWindowRenderOneSentence(t *testing.T) {
	view, err := buildSummaryView(SummaryData{Range: weekRange(t)})
	if err != nil {
		t.Fatalf("buildSummaryView: %v", err)
	}

	var out strings.Builder
	if err := insightpages.SummaryPanels(view).Render(context.Background(), &out); err != nil {
		t.Fatalf("render: %v", err)
	}

	html := out.String()
	if !strings.Contains(html, "Nothing logged") {
		t.Error("an empty window does not say so")
	}
	if strings.Contains(html, "data-echarts") {
		t.Error("an empty window still drew a ring")
	}
}
