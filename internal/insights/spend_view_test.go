package insights

import (
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/spend"
	insightpages "github.com/NorthAIProject/north-client/web/insights"
)

func mustSpendView(t *testing.T, data SpendData) insightpages.SpendView {
	t.Helper()
	view, err := buildSpendView(data)
	if err != nil {
		t.Fatalf("buildSpendView: %v", err)
	}
	return view
}

func spent(t *testing.T) SpendData {
	t.Helper()
	return SpendData{
		Range: weekRange(t),
		Surfaces: []spend.SurfaceSpend{
			{Surface: "coach", Generations: 40, InputTokens: 80000, OutputTokens: 20000, CostMicros: 300000},
			{Surface: "telegram", Generations: 10, InputTokens: 20000, OutputTokens: 5000, CostMicros: 100000},
		},
		Models: []spend.ModelSpend{
			{Provider: "anthropic", Model: "claude-sonnet-5", Generations: 45, CostMicros: 380000},
			{Provider: "anthropic", Model: "claude-haiku-4-5", Generations: 5, CostMicros: 20000},
		},
	}
}

func TestSpendViewTotalsAcrossSurfaces(t *testing.T) {
	view := mustSpendView(t, spent(t))

	if view.Generations != 50 {
		t.Errorf("Generations = %d, want 50", view.Generations)
	}
	if view.TotalTokens != 125000 {
		t.Errorf("TotalTokens = %d, want 125000", view.TotalTokens)
	}
	if view.TotalCost == "" {
		t.Error("no total cost rendered")
	}
}

func TestSpendViewRowsCarryTheirShare(t *testing.T) {
	view := mustSpendView(t, spent(t))

	if len(view.Surfaces) != 2 {
		t.Fatalf("surface rows = %d, want 2", len(view.Surfaces))
	}
	// coach is 300000 of 400000 micros.
	if view.Surfaces[0].Pct != 75 {
		t.Errorf("first surface share = %d%%, want 75%%", view.Surfaces[0].Pct)
	}
	if view.Surfaces[0].Label == "" || view.Surfaces[0].Cost == "" {
		t.Errorf("surface row is not rendered: %+v", view.Surfaces[0])
	}
}

func TestSpendViewNamesTheModel(t *testing.T) {
	view := mustSpendView(t, spent(t))

	if len(view.Models) != 2 {
		t.Fatalf("model rows = %d, want 2", len(view.Models))
	}
	if view.Models[0].Label != "claude-sonnet-5" {
		t.Errorf("model label = %q, want the model name", view.Models[0].Label)
	}
}

func TestSpendViewOfAnEmptyWindowSaysSo(t *testing.T) {
	view := mustSpendView(t, SpendData{Range: weekRange(t)})

	if view.HasData {
		t.Error("HasData = true with nothing spent")
	}
}

func TestSpendViewSurvivesAFreeWindow(t *testing.T) {
	// Transcription moved to the cluster's own service, so a window can hold
	// real generations at zero cost. The shares must not divide by zero.
	view := mustSpendView(t, SpendData{
		Range:    weekRange(t),
		Surfaces: []spend.SurfaceSpend{{Surface: "telegram_voice", Generations: 12, CostMicros: 0}},
	})

	if !view.HasData {
		t.Error("HasData = false despite twelve generations")
	}
	if view.Surfaces[0].Pct != 0 {
		t.Errorf("share = %d%%, want 0 with nothing spent", view.Surfaces[0].Pct)
	}
}

func TestSpendViewMarksTheCurrency(t *testing.T) {
	// spend.Euros returns a bare number, which reads fine in a CLI column
	// under a "cost" heading and reads as anything at all in a stat tile.
	view := mustSpendView(t, spent(t))

	if !strings.HasPrefix(view.TotalCost, "€") {
		t.Errorf("TotalCost = %q, want a currency mark", view.TotalCost)
	}
	for _, r := range view.Surfaces {
		if !strings.HasPrefix(r.Cost, "€") {
			t.Errorf("surface %q cost = %q, want a currency mark", r.Label, r.Cost)
		}
	}
}
