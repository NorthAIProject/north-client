package layout

import (
	"context"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

func hrefs(groups []NavGroup) (kept []string, more []string) {
	for _, g := range groups {
		for _, item := range g.Items {
			if g.More {
				more = append(more, item.Href)
			} else {
				kept = append(kept, item.Href)
			}
		}
	}
	return kept, more
}

// A first week keeps four places in view and folds the rest, closed.
func TestTheFirstWeekFoldsTheRailUnderMore(t *testing.T) {
	ctx := WithSimpleNav(i18n.WithLocale(context.Background(), "en"))
	groups := BuildNav(ctx, "/app")

	kept, more := hrefs(groups)
	if len(kept) != len(firstWeekNav) {
		t.Errorf("kept %v, want %v", kept, firstWeekNav)
	}
	full, _ := hrefs(BuildNav(context.Background(), "/app"))
	if len(kept)+len(more) != len(full) {
		t.Errorf("fold lost items: %d kept + %d more, want %d", len(kept), len(more), len(full))
	}
	last := groups[len(groups)-1]
	if !last.More || last.Open || last.Label != "More" {
		t.Errorf("last group = %+v, want a closed More", last)
	}
}

func TestMoreOpensOnAPageInsideIt(t *testing.T) {
	groups := BuildNav(WithSimpleNav(context.Background()), "/app/mind")
	if last := groups[len(groups)-1]; !last.More || !last.Open {
		t.Errorf("More should be open on /app/mind: %+v", last)
	}
}

func TestTheFullRailIsUnchangedWithoutTheFlag(t *testing.T) {
	for _, g := range BuildNav(context.Background(), "/app") {
		if g.More {
			t.Fatal("a More group without a first week")
		}
	}
}
