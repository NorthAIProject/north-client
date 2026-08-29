package layout

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/web/shared/ui/icon"
)

// The invariant that makes the registry a registry. Two rows for one page and
// the palette shows a duplicate, the route test's set comparison goes quiet,
// and BuildNav's parent lookup picks whichever came second.
func TestNoTwoDestinationsShareAnHref(t *testing.T) {
	t.Parallel()

	seen := map[string]string{}
	for _, d := range Destinations() {
		if first, ok := seen[d.Href]; ok {
			t.Errorf("%q and %q both point at %s", first, d.Label, d.Href)
			continue
		}
		seen[d.Href] = d.Label
	}
}

func TestEveryDestinationIsComplete(t *testing.T) {
	t.Parallel()

	groups := map[string]bool{}
	for _, g := range GroupOrder() {
		groups[g] = true
	}

	for _, d := range Destinations() {
		if d.Label == "" {
			t.Errorf("%s has no label", d.Href)
		}
		if d.Description == "" {
			t.Errorf("%q has no description", d.Label)
		}
		if d.Icon == "" {
			t.Errorf("%q has no icon", d.Label)
		}
		if !strings.HasPrefix(d.Href, "/app") {
			t.Errorf("%q points outside the application: %s", d.Label, d.Href)
		}
		// A destination you can only reach if you already know an id is not a
		// destination. The palette has nothing to substitute for the param.
		if strings.ContainsAny(d.Href, "{}*") {
			t.Errorf("%q has a path parameter: %s", d.Label, d.Href)
		}
		if !groups[d.Group] {
			t.Errorf("%q is in group %q, which is not in GroupOrder()", d.Label, d.Group)
		}
	}
}

// The highest-value test here, and the reason Icon is a required field.
//
// icon.Icon returns an error for a name it does not know. templ flushes
// nothing on error, so a typo like "chart-lines" does not render a broken
// image — it makes every signed-in page answer 200 with an empty body.
func TestEveryDestinationIconRenders(t *testing.T) {
	t.Parallel()

	for _, d := range Destinations() {
		if err := icon.Icon(d.Icon)().Render(context.Background(), io.Discard); err != nil {
			t.Errorf("%q uses icon %q, which does not render: %v", d.Label, d.Icon, err)
		}
	}
}

// Keywords are matched against a lowercased haystack, so an uppercase one can
// never match anything. A keyword equal to its own label is dead weight: the
// label is already matched, and at a higher rank.
func TestKeywordsAreUsable(t *testing.T) {
	t.Parallel()

	for _, d := range Destinations() {
		seen := map[string]bool{}
		for _, k := range d.Keywords {
			switch {
			case k != strings.ToLower(k):
				t.Errorf("%q has an uppercase keyword %q, which can never match", d.Label, k)
			case strings.TrimSpace(k) == "":
				t.Errorf("%q has an empty keyword", d.Label)
			case k == strings.ToLower(d.Label):
				t.Errorf("%q repeats its own label as a keyword", d.Label)
			case seen[k]:
				t.Errorf("%q lists the keyword %q twice", d.Label, k)
			}
			seen[k] = true
		}
	}
}

// A nested item whose parent is not in the rail would silently vanish.
func TestEveryNestedNavItemHasAParent(t *testing.T) {
	t.Parallel()

	parents := map[string]string{}
	for _, d := range Destinations() {
		if d.Nav.Show && d.Nav.Under == "" {
			parents[d.Href] = d.Group
		}
	}

	for _, d := range Destinations() {
		if !d.Nav.Show || d.Nav.Under == "" {
			continue
		}
		group, ok := parents[d.Nav.Under]
		if !ok {
			t.Errorf("%q nests under %s, which is not a top-level nav item", d.Label, d.Nav.Under)
			continue
		}
		if group != d.Group {
			t.Errorf("%q is in group %q but nests under an item in %q", d.Label, d.Group, group)
		}
	}
}

// BuildNav used to be a hand-written literal and is now a fold over the
// registry. This pins the result so that refactor is provably a refactor, and
// stays one: the sidebar is what every signed-in page renders, and a mistake
// in it is not subtle but it is easy to ship.
func TestBuildNavMatchesTheSidebarAsShipped(t *testing.T) {
	t.Parallel()

	type item struct {
		label    string
		href     string
		children []string
	}
	want := []struct {
		group string
		items []item
	}{
		{"Today", []item{
			{"Overview", "/app", nil},
			{"Check-ins", "/app/check-ins", nil},
			{"Coach", "/app/chat", nil},
		}},
		{"Body", []item{
			{"Fitness", "/app/fitness", nil},
			{"Care", "/app/care", nil},
		}},
		{"Mind", []item{
			{"Mind", "/app/mind", nil},
			{"Memory", "/app/memories", nil},
			{"Decisions", "/app/decisions", nil},
		}},
		{"Progress", []item{
			{"Goals", "/app/goals", nil},
			{"Reports", "/app/reports", nil},
			{"Insights", "/app/insights/timeline", []string{
				"Activity", "Body", "Mind", "Progress", "Training",
			}},
		}},
		{"System", []item{
			{"Knowledge", "/app/knowledge", nil},
			{"Settings", "/app/settings", nil},
		}},
	}

	got := BuildNav("/app")
	if len(got) != len(want) {
		t.Fatalf("BuildNav returned %d groups, want %d", len(got), len(want))
	}

	for gi, w := range want {
		g := got[gi]
		if g.Label != w.group {
			t.Errorf("group %d is %q, want %q", gi, g.Label, w.group)
		}
		if len(g.Items) != len(w.items) {
			t.Errorf("group %q has %d items, want %d", w.group, len(g.Items), len(w.items))
			continue
		}
		for ii, wi := range w.items {
			it := g.Items[ii]
			if it.Label != wi.label || it.Href != wi.href {
				t.Errorf("group %q item %d is %q (%s), want %q (%s)",
					w.group, ii, it.Label, it.Href, wi.label, wi.href)
			}
			if it.Icon == "" {
				t.Errorf("%q has no icon, so it disappears from the collapsed rail", it.Label)
			}
			if len(it.Children) != len(wi.children) {
				t.Errorf("%q has %d children, want %d", wi.label, len(it.Children), len(wi.children))
				continue
			}
			for ci, wc := range wi.children {
				if it.Children[ci].Label != wc {
					t.Errorf("%q child %d is %q, want %q", wi.label, ci, it.Children[ci].Label, wc)
				}
			}
		}
	}
}

// A section stays lit while you are inside it, which is what tells you where
// you are once the page itself scrolls.
func TestAChildPageLightsItsParentSection(t *testing.T) {
	t.Parallel()

	var insights NavItem
	for _, g := range BuildNav("/app/insights/body") {
		for _, item := range g.Items {
			if item.Href == "/app/insights/timeline" {
				insights = item
			}
		}
	}

	if insights.Href == "" {
		t.Fatal("the Insights item is missing from the nav")
	}
	if !insights.Active {
		t.Error("Insights is not marked active while one of its children is the page")
	}

	var body NavItem
	for _, c := range insights.Children {
		if c.Href == "/app/insights/body" {
			body = c
		}
	}
	if !body.Active {
		t.Error("the Body child is not marked active on its own page")
	}
}

// Active state is an exact Href match, so a page that passes something not in
// the registry lights nothing. Cheap to get wrong, invisible when you do.
func TestSettingsSubPagesLightTheSettingsItem(t *testing.T) {
	t.Parallel()

	for _, g := range BuildNav("/app/settings") {
		for _, item := range g.Items {
			if item.Href == "/app/settings" && !item.Active {
				t.Error("Settings is not active on its own page")
			}
		}
	}
}
