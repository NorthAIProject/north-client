package goals

import (
	"github.com/NorthAIProject/north-client/internal/shared/viz"
	goalpages "github.com/NorthAIProject/north-client/web/goals"
)

func buildInstruments(list []Goal, overdueMilestones int) (goalpages.Instruments, error) {
	activeCount := 0
	progressSum := 0
	progressN := 0
	byCategory := make(map[string]int)

	for _, g := range list {
		if !g.IsActive() {
			continue
		}
		activeCount++
		if pct, ok := g.Progress(); ok {
			progressSum += pct
			progressN++
		}
		cat := g.Category
		if cat == "" {
			cat = CategoryOther
		}
		byCategory[cat]++
	}

	avgProgress := 0
	if progressN > 0 {
		avgProgress = progressSum / progressN
	}

	segments := make([]viz.DonutSegment, 0, len(Categories))
	for _, cat := range Categories {
		if n := byCategory[cat]; n > 0 {
			segments = append(segments, viz.DonutSegment{
				Label: categoryLabel(cat),
				Value: n,
			})
		}
	}

	var donut map[string]any
	hasDonut := len(segments) > 0
	if hasDonut {
		raw, err := viz.DonutOptionJSON(segments)
		if err != nil {
			return goalpages.Instruments{}, err
		}
		donut, err = viz.UnmarshalOption(raw)
		if err != nil {
			return goalpages.Instruments{}, err
		}
	}

	return goalpages.Instruments{
		ActiveCount:       activeCount,
		AvgProgress:       avgProgress,
		OverdueMilestones: overdueMilestones,
		CategoryDonut:     donut,
		HasCategoryDonut:  hasDonut,
	}, nil
}

func categoryLabel(category string) string {
	switch category {
	case CategoryFitness:
		return "Fitness"
	case CategoryHealth:
		return "Health"
	case CategoryWork:
		return "Work"
	case CategoryLearning:
		return "Learning"
	case CategoryPersonal:
		return "Personal"
	default:
		return "Other"
	}
}
