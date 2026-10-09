package exercises

import (
	"context"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
)

func TestMatcherFoldsSpellingAndRefusesAmbiguity(t *testing.T) {
	t.Parallel()
	m := NewMatcher([]Exercise{
		{Slug: "barbell-bench-press-medium-grip", Name: "Barbell Bench Press - Medium Grip"},
		{Slug: "dumbbell-bench-press", Name: "Dumbbell Bench Press"},
		{Slug: "pull-up", Name: "Pull-Up"},
		{Slug: "hammer-curl", Name: "Hammer Curl"},
		{Slug: "rope-hammer-curl", Name: "Rope Hammer Curl"},
		{Slug: "cable-row", Name: "Cable Row Wide"},
		{Slug: "machine-row", Name: "Machine Row Wide"},
	})
	for _, tc := range []struct {
		name, want string
	}{
		{"dumbbell-bench-press", "dumbbell-bench-press"},             // a slug as-is
		{"Bench Press (Dumbbell)", "dumbbell-bench-press"},           // same words, other order
		{"DB bench presses", "dumbbell-bench-press"},                 // abbreviation and plural
		{"pullups", "pull-up"},                                       // closed compound
		{"Hammer Curls", "hammer-curl"},                              // exact beats a superset
		{"Bench Press (Barbell)", "barbell-bench-press-medium-grip"}, // alias
		{"Row", ""},           // two catalog rows are equally close
		{"Zercher carry", ""}, // nothing like it
		{"   ", ""},
	} {
		got, ok := m.Match(tc.name)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("Match(%q) = %q, %v; want %q", tc.name, got, ok, tc.want)
		}
	}
}

func TestAliasesPointAtCatalog(t *testing.T) {
	t.Parallel()
	svc := NewService(NewRepository(testdb.New(t)))
	m, err := svc.Matcher(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for name, slug := range otherAppNames {
		if !m.slugs[slug] {
			t.Errorf("alias %q points at %q, which is not in the catalog", name, slug)
			continue
		}
		if got, ok := m.Match(name); !ok || got != slug {
			t.Errorf("Match(%q) = %q, want %q", name, got, slug)
		}
	}
}
