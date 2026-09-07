package layout

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Every destination must have a catalogue key, or it silently stays English in
// every language while looking perfectly fine in review.
func TestEveryDestinationHasACatalogueKey(t *testing.T) {
	for _, d := range Destinations() {
		if d.Key == "" {
			t.Errorf("%q (%s) has no i18n key", d.Label, d.Href)
			continue
		}
		if !strings.HasPrefix(d.Key, "nav.") {
			t.Errorf("%q: key %q should be under the nav. stem", d.Label, d.Key)
		}
	}
}

// The English lives twice: in this table and in the catalogue. That is a
// deliberate trade — a caller with no request still reads a sentence — and this
// is the test that stops the two copies drifting.
func TestNavCatalogueMatchesTheEnglish(t *testing.T) {
	ctx := i18n.WithLocale(context.Background(), users.LocaleEN)

	for _, d := range Destinations() {
		if got := d.LabelIn(ctx); got != d.Label {
			t.Errorf("%s: catalogue label %q, table says %q", d.Key, got, d.Label)
		}
		if got := d.DescriptionIn(ctx); got != d.Description {
			t.Errorf("%s: catalogue description %q, table says %q", d.Key, got, d.Description)
		}
		if d.Nav.Label != "" {
			if got := d.NavLabelIn(ctx); got != d.Nav.Label {
				t.Errorf("%s: catalogue rail label %q, table says %q", d.Key, got, d.Nav.Label)
			}
		}
		if d.Nav.SelfChildLabel != "" {
			if got := d.SelfChildLabelIn(ctx); got != d.Nav.SelfChildLabel {
				t.Errorf("%s: catalogue self-child label %q, table says %q", d.Key, got, d.Nav.SelfChildLabel)
			}
		}
	}
}

// Every group heading must be translated in every language, since the rail
// renders all five on every page.
func TestEveryGroupHeadingIsTranslated(t *testing.T) {
	for _, locale := range users.Locales {
		ctx := i18n.WithLocale(context.Background(), locale)
		for _, group := range GroupOrder() {
			got := GroupLabel(ctx, group)
			if got == "" || strings.HasPrefix(got, "nav.group.") {
				t.Errorf("%s: group %q has no heading (got %q)", locale, group, got)
			}
		}
	}
}

// The rail is assembled already translated. This is the end-to-end check that
// the locale on the context actually reaches it.
func TestBuildNavRendersInTheRequestLocale(t *testing.T) {
	ctx := i18n.WithLocale(context.Background(), users.LocalePTBR)

	groups := BuildNav(ctx, "/app")
	if len(groups) == 0 {
		t.Fatal("no nav groups")
	}
	if groups[0].Label != "Hoje" {
		t.Errorf("first group heading = %q, want %q", groups[0].Label, "Hoje")
	}

	var found bool
	for _, g := range groups {
		for _, item := range g.Items {
			if item.Href == "/app" {
				found = true
				if item.Label != "Visão geral" {
					t.Errorf("Overview label = %q, want %q", item.Label, "Visão geral")
				}
			}
		}
	}
	if !found {
		t.Error("the overview item is missing from the rail")
	}
}

// A Portuguese reader still knows this product has "settings" in it — from the
// docs, from a URL, from a colleague. Typing the English must not come up
// empty, so the haystack carries both.
func TestTheHaystackIsSearchableInBothLanguages(t *testing.T) {
	ctx := i18n.WithLocale(context.Background(), users.LocalePTBR)

	for _, d := range Destinations() {
		if d.Href != "/app/settings" {
			continue
		}
		hay := haystack(ctx, d)
		for _, want := range []string{"settings", "configurações"} {
			if !strings.Contains(hay, want) {
				t.Errorf("haystack %q does not contain %q", hay, want)
			}
		}
		return
	}
	t.Fatal("no settings destination to check")
}
