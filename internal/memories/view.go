package memories

import (
	"github.com/NorthAIProject/north-client/internal/memories/memory"
	"github.com/NorthAIProject/north-client/internal/shared/viz"
	memorypages "github.com/NorthAIProject/north-client/web/memories"
)

func buildInstruments(pending, approved []Memory) (memorypages.Instruments, error) {
	pinned := 0
	byCategory := make(map[string]int)
	for _, m := range approved {
		if m.Pinned {
			pinned++
		}
		cat := m.Category
		if cat == "" {
			cat = memory.CategoryGeneral
		}
		byCategory[cat]++
	}

	segments := make([]viz.DonutSegment, 0, len(memory.Categories))
	for _, cat := range memory.Categories {
		if n := byCategory[cat]; n > 0 {
			segments = append(segments, viz.DonutSegment{Label: cat, Value: n})
		}
	}

	var donut map[string]any
	hasDonut := len(segments) > 0
	if hasDonut {
		raw, err := viz.DonutOptionJSON(segments)
		if err != nil {
			return memorypages.Instruments{}, err
		}
		donut, err = viz.UnmarshalOption(raw)
		if err != nil {
			return memorypages.Instruments{}, err
		}
	}

	return memorypages.Instruments{
		PendingCount:     len(pending),
		ApprovedCount:    len(approved),
		PinnedCount:      pinned,
		CategoryDonut:    donut,
		HasCategoryDonut: hasDonut,
	}, nil
}
