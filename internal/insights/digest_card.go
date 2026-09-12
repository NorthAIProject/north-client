package insights

import (
	"fmt"

	"github.com/NorthAIProject/north-client/internal/shared/rasterize"
	insightpages "github.com/NorthAIProject/north-client/web/insights"
)

// digestCard turns the page's own view into the picture a digest carries.
//
// Built from the SummaryView rather than from the data, for the same reason
// the digest's words are: the card and the page must say the same thing, and
// the only way to be sure of that is for both to read the same view.
func digestCard(view insightpages.SummaryView) rasterize.Card {
	card := rasterize.Card{Title: view.Range.Label}

	if view.Judged > 0 {
		card.Subtitle = fmt.Sprintf("%d of %d on track", view.OnTrack, view.Judged)
	}

	for _, s := range view.Scores {
		card.Rings = append(card.Rings, rasterize.Ring{
			Label:   s.Label,
			Points:  s.Points,
			HasData: s.HasData,
		})
	}

	// One series, not four. The card is read on a phone in a chat, and every
	// other number is one tap away on the page it links to.
	for _, p := range view.Pinned {
		if !p.HasChart || len(p.Chart.Data.Datasets) == 0 {
			continue
		}
		card.Bars = rasterize.Bars{
			Label:  p.Label,
			Values: p.Chart.Data.Datasets[0].Data,
			Labels: p.Chart.Data.Labels,
		}
		break
	}

	return card
}
