package layout

import (
	"context"
	"slices"
)

type simpleNavKey struct{}

// WithSimpleNav marks a request whose person is in their first week
// (internal/firstweek). Their rail shows the few places the week uses and
// folds the rest under More, closed: thirty features is no first step.
func WithSimpleNav(ctx context.Context) context.Context {
	return context.WithValue(ctx, simpleNavKey{}, true)
}

func simpleNav(ctx context.Context) bool {
	on, _ := ctx.Value(simpleNavKey{}).(bool)
	return on
}

// firstWeekNav are the rail items a first week keeps in view.
var firstWeekNav = []string{"/app", "/app/chat", "/app/check-ins", "/app/goals"}

// foldUnderMore keeps the first-week items in their groups and moves every
// other item into one More group, open only when the current page is in it.
func foldUnderMore(ctx context.Context, groups []NavGroup) []NavGroup {
	kept := make([]NavGroup, 0, len(groups)+1)
	more := NavGroup{Label: GroupLabel(ctx, "more"), More: true}
	for _, g := range groups {
		keep := NavGroup{Label: g.Label}
		for _, item := range g.Items {
			if slices.Contains(firstWeekNav, item.Href) {
				keep.Items = append(keep.Items, item)
				continue
			}
			more.Items = append(more.Items, item)
			if item.Active {
				more.Open = true
			}
		}
		if len(keep.Items) > 0 {
			kept = append(kept, keep)
		}
	}
	if len(more.Items) > 0 {
		kept = append(kept, more)
	}
	return kept
}
