package insights

import (
	"math"

	"github.com/NorthAIProject/north-client/internal/insights/highlight"
	"github.com/NorthAIProject/north-client/internal/shared/viz"
	insightpages "github.com/NorthAIProject/north-client/web/insights"
)

func buildCoachView(data CoachData) (insightpages.CoachView, error) {
	loc := data.Range.Location()
	labels := bucketLabels(data.Range)

	var yours, theirs []point
	var rated, helpful int

	for _, m := range data.Messages {
		at := m.At.In(loc)
		// Asked rather than compared: a tool call and a tool result are
		// machinery rather than conversation, and neither predicate claims
		// them.
		switch {
		case m.IsUser():
			yours = append(yours, point{At: at, Value: 1})
		case m.IsModel():
			theirs = append(theirs, point{At: at, Value: 1})
		}

		// Rated replies only. A reply nobody marked is not a thumbs-down, and
		// counting it as one would make the coach look worse the more it was
		// used — which is exactly backwards.
		if m.Helpful == nil {
			continue
		}
		rated++
		if *m.Helpful {
			helpful++
		}
	}

	view := insightpages.CoachView{
		Range:        rangeView(data.Range),
		Turns:        len(yours) + len(theirs),
		YourMessages: len(yours),
		CoachReplies: len(theirs),
		Rated:        rated,
		HasRatings:   rated > 0,
		Truncated:    data.Truncated,
		HasData:      len(data.Messages) > 0,
	}

	if rated > 0 {
		view.HelpfulRate = int(math.Round(float64(helpful) / float64(rated) * 100))
	}

	yourSeries := bucketed(data.Range, yours)
	view.Chart = viz.GroupedBar("insights-coach-turns", labels, []viz.BarSeries{
		{Label: "You", Values: yourSeries},
		{Label: "Coach", Values: bucketed(data.Range, theirs)},
	})

	view.Highlights = highlight.Find(highlight.Input{
		Series: []highlight.Series{{
			Label: "Messages", Decimals: 0,
			Period: periodNoun(data.Range), Labels: labels, Values: yourSeries,
		}},
	}, maxHighlights)

	return view, nil
}
