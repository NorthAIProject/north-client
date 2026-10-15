package insights

import (
	"context"

	"github.com/NorthAIProject/north-client/internal/spend"
	insightpages "github.com/NorthAIProject/north-client/web/insights"
)

func buildSpendView(ctx context.Context, data SpendData) (insightpages.SpendView, error) {
	var totalCost, totalTokens, generations int64
	for _, s := range data.Surfaces {
		totalCost += s.CostMicros
		totalTokens += s.InputTokens + s.OutputTokens
		generations += s.Generations
	}

	view := insightpages.SpendView{
		Range:       rangeView(ctx, data.Range),
		TotalCost:   euros(totalCost),
		TotalTokens: totalTokens,
		Generations: generations,
		HasData:     generations > 0,
	}

	for _, s := range data.Surfaces {
		view.Surfaces = append(view.Surfaces, insightpages.SpendRow{
			Label:       surfaceLabel(ctx, s.Surface),
			Cost:        euros(s.CostMicros),
			Generations: s.Generations,
			Tokens:      s.InputTokens + s.OutputTokens,
			Pct:         share(s.CostMicros, totalCost),
		})
	}
	for _, m := range data.Models {
		view.Models = append(view.Models, insightpages.SpendRow{
			Label:       modelLabel(m),
			Cost:        euros(m.CostMicros),
			Generations: m.Generations,
			Tokens:      m.InputTokens + m.OutputTokens,
			Pct:         share(m.CostMicros, totalCost),
		})
	}

	return view, nil
}

// euros marks the currency spend.Euros leaves off.
//
// That function returns a bare number because its caller is a CLI column under
// a heading that already says what it is. On a page the number stands alone,
// and "0.08" on its own could be anything.
func euros(micros int64) string { return "€" + spend.Euros(micros) }

// share is a row's percentage of the window's cost.
//
// Zero when the window cost nothing, which is a real state rather than a
// guard against the impossible: transcription runs on the cluster's own
// service now, so a window can hold real generations at no cost at all.
func share(part, total int64) int {
	if total <= 0 {
		return 0
	}
	return int(part * 100 / total)
}

// surfaceLabel turns a spend surface constant into something readable. An
// unknown surface renders its own name rather than being dropped — a new one
// appearing in this list is information, not a bug to hide.
func surfaceLabel(ctx context.Context, surface string) string {
	return translated(ctx, "insights.spend.surface."+surface, surface)
}

// modelLabel prefers the model name, falling back to the provider for a
// generation whose model the provider never named.
func modelLabel(m spend.ModelSpend) string {
	if m.Model != "" {
		return m.Model
	}
	return m.Provider
}
