package documents

import (
	"github.com/NorthAIProject/north-client/internal/documents/document"
	"github.com/NorthAIProject/north-client/internal/shared/viz"
	knowledgepages "github.com/NorthAIProject/north-client/web/knowledge"
)

func buildInstruments(counts document.Counts) (knowledgepages.Instruments, error) {
	segments := []viz.DonutSegment{
		{Label: "Read", Value: counts.Ready},
		{Label: "Waiting", Value: counts.Pending},
		{Label: "Unreadable", Value: counts.Failed},
	}

	var donut map[string]any
	hasDonut := counts.Ready+counts.Pending+counts.Failed > 0
	if hasDonut {
		raw, err := viz.DonutOptionJSON(segments)
		if err != nil {
			return knowledgepages.Instruments{}, err
		}
		donut, err = viz.UnmarshalOption(raw)
		if err != nil {
			return knowledgepages.Instruments{}, err
		}
	}

	return knowledgepages.Instruments{
		IndexHealthDonut: donut,
		HasDonut:         hasDonut,
	}, nil
}
