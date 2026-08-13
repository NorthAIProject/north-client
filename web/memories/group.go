package memories

import (
	"sort"

	"github.com/NorthAIProject/north-client/internal/memories/memory"
)

// CategoryGroup is approved facts under one category label.
type CategoryGroup struct {
	Category string
	Items    []memory.Memory
}

// GroupApprovedByCategory orders approved facts for the settings UI.
//
// Categories follow the product list; within each group pinned facts come first,
// then active coaching facts, then excluded ones.
func GroupApprovedByCategory(list []memory.Memory) []CategoryGroup {
	if len(list) == 0 {
		return nil
	}

	byCat := make(map[string][]memory.Memory)
	for _, m := range list {
		cat := m.Category
		if cat == "" {
			cat = memory.CategoryGeneral
		}
		byCat[cat] = append(byCat[cat], m)
	}

	order := append([]string(nil), memory.Categories...)
	seen := make(map[string]bool, len(order))
	for _, cat := range order {
		seen[cat] = true
	}

	var extra []string
	for cat := range byCat {
		if !seen[cat] {
			extra = append(extra, cat)
		}
	}
	sort.Strings(extra)
	order = append(order, extra...)

	out := make([]CategoryGroup, 0, len(byCat))
	for _, cat := range order {
		items, ok := byCat[cat]
		if !ok {
			continue
		}
		sort.SliceStable(items, func(i, j int) bool {
			a, b := items[i], items[j]
			if a.Pinned != b.Pinned {
				return a.Pinned
			}
			if a.Excluded != b.Excluded {
				return !a.Excluded
			}
			return a.UpdatedAt.After(b.UpdatedAt)
		})
		out = append(out, CategoryGroup{Category: cat, Items: items})
	}
	return out
}
