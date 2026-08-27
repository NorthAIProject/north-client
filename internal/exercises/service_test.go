package exercises_test

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/exercises"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

// The catalog is seeded by migration, so a freshly migrated test database
// already contains it. These tests therefore assert against the real seed
// rather than fixtures — which is the point: the seed is the thing most likely
// to be wrong, and a fixture would prove nothing about it.
func newService(t *testing.T) *exercises.Service {
	t.Helper()
	return exercises.NewService(exercises.NewRepository(testdb.New(t)))
}

func TestSeededCatalogIsPresentAndCoherent(t *testing.T) {
	t.Parallel()

	svc := newService(t)

	all, total, err := svc.Search(context.Background(), exercises.Filter{Limit: 200})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if total < 150 {
		t.Fatalf("the catalog looks unseeded: %d exercises", total)
	}

	seen := map[string]bool{}
	for _, e := range all {
		if e.Slug == "" || e.Name == "" {
			t.Errorf("exercise %q has an empty slug or name", e.ID)
		}
		if seen[e.Slug] {
			t.Errorf("duplicate slug %q", e.Slug)
		}
		seen[e.Slug] = true

		if len(e.Primary) == 0 {
			t.Errorf("%q has no primary muscle, so the viewer would highlight nothing", e.Slug)
		}
		for _, key := range append(append([]string{}, e.Primary...), e.Secondary...) {
			if !plan.IsMuscleGroup(key) {
				t.Errorf("%q carries muscle key %q, which is outside plan.MuscleGroups", e.Slug, key)
			}
		}
	}
}

// A muscle in both lists would be drawn twice at two intensities, and the
// weaker one could win — an exercise's main target rendered as a secondary.
func TestPrimaryAndSecondaryNeverOverlap(t *testing.T) {
	t.Parallel()

	all, _, err := newService(t).Search(context.Background(), exercises.Filter{Limit: 200})
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	for _, e := range all {
		for _, secondary := range e.Secondary {
			for _, primary := range e.Primary {
				if secondary == primary {
					t.Errorf("%q lists %q as both primary and secondary", e.Slug, secondary)
				}
			}
		}
	}
}

func TestSearchFiltersByNameMuscleAndEquipment(t *testing.T) {
	t.Parallel()

	svc := newService(t)
	ctx := context.Background()

	byName, _, err := svc.Search(ctx, exercises.Filter{Query: "deadlift"})
	if err != nil {
		t.Fatalf("search by name: %v", err)
	}
	if len(byName) == 0 {
		t.Fatal("expected the catalog to contain deadlifts")
	}
	for _, e := range byName {
		if !strings.Contains(strings.ToLower(e.Name), "deadlift") {
			t.Errorf("%q matched a search for deadlift", e.Name)
		}
	}

	byMuscle, _, err := svc.Search(ctx, exercises.Filter{Muscle: "lats"})
	if err != nil {
		t.Fatalf("search by muscle: %v", err)
	}
	if len(byMuscle) == 0 {
		t.Fatal("expected the catalog to contain lat work")
	}
	for _, e := range byMuscle {
		if !contains(e.Primary, "lats") && !contains(e.Secondary, "lats") {
			t.Errorf("%q matched a search for lats but lists neither primary nor secondary lats", e.Slug)
		}
	}

	byEquipment, _, err := svc.Search(ctx, exercises.Filter{Equipment: []string{"dumbbell"}})
	if err != nil {
		t.Fatalf("search by equipment: %v", err)
	}
	if len(byEquipment) == 0 {
		t.Fatal("expected the catalog to contain dumbbell work")
	}
	for _, e := range byEquipment {
		if e.Equipment != "dumbbell" {
			t.Errorf("%q has equipment %q, not dumbbell", e.Slug, e.Equipment)
		}
	}
}

// A filter value outside the vocabulary is dropped rather than passed through,
// so a hand-edited URL returns the unfiltered page instead of an empty one
// that looks like the catalog is broken.
func TestSearchIgnoresFilterValuesOutsideTheVocabulary(t *testing.T) {
	t.Parallel()

	svc := newService(t)
	ctx := context.Background()

	_, total, err := svc.Search(ctx, exercises.Filter{})
	if err != nil {
		t.Fatalf("unfiltered search: %v", err)
	}

	_, nonsenseTotal, err := svc.Search(ctx, exercises.Filter{Muscle: "pecs", Category: "not-a-category"})
	if err != nil {
		t.Fatalf("nonsense search: %v", err)
	}

	if nonsenseTotal != total {
		t.Errorf("unrecognised filters should be dropped: got %d, want the unfiltered %d", nonsenseTotal, total)
	}
}

// Bodyweight belongs in every candidate list. Leaving it out would mean
// someone with a pair of dumbbells is never offered a push-up.
func TestCandidatesAlwaysIncludeBodyweight(t *testing.T) {
	t.Parallel()

	candidates, err := newService(t).Candidates(context.Background(), []string{"dumbbell"})
	if err != nil {
		t.Fatalf("candidates: %v", err)
	}
	if len(candidates) == 0 {
		t.Fatal("expected candidates for someone with dumbbells")
	}

	var bodyweight, dumbbell int
	for _, e := range candidates {
		switch e.Equipment {
		case exercises.EquipmentNone:
			bodyweight++
		case "dumbbell":
			dumbbell++
		default:
			t.Errorf("%q needs %q, which this person does not have", e.Slug, e.Equipment)
		}
	}

	if bodyweight == 0 {
		t.Error("no bodyweight exercises offered")
	}
	if dumbbell == 0 {
		t.Error("no dumbbell exercises offered")
	}
}

// Every candidate must survive the plan validator, or the catalog would
// recommend exercises the generated plan is then rejected for using.
func TestEveryCandidateSatisfiesThePlanValidator(t *testing.T) {
	t.Parallel()

	owned := []string{"dumbbell", "barbell", "bench"}

	candidates, err := newService(t).Candidates(context.Background(), owned)
	if err != nil {
		t.Fatalf("candidates: %v", err)
	}

	intake := plan.Intake{
		Goal: "strength", Experience: "intermediate",
		DaysPerWeek: 1, SessionMinutes: 60,
		Equipment: owned,
	}

	for _, e := range candidates {
		p := plan.Plan{
			Name: "Catalog check", Rationale: "Checking one exercise.", WeeksTotal: 4,
			Days: []plan.PlanDay{{
				Weekday: "Monday", Focus: "full body",
				Exercises: []plan.Exercise{{
					Name: e.Name, Sets: 3, Reps: "8-12", RestSeconds: 90, Equipment: e.Equipment,
				}},
			}},
		}
		if problems := plan.Validate(p, intake); len(problems) != 0 {
			t.Errorf("catalog offers %q (equipment %q) but the validator rejects it: %v", e.Name, e.Equipment, problems)
		}
	}
}

func TestResolveReturnsOnlyKnownSlugs(t *testing.T) {
	t.Parallel()

	svc := newService(t)
	ctx := context.Background()

	found, err := svc.Resolve(ctx, []string{"barbell-deadlift", "not-a-real-exercise", "BARBELL-DEADLIFT"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if _, ok := found["barbell-deadlift"]; !ok {
		t.Error("expected barbell-deadlift to resolve")
	}
	if _, ok := found["not-a-real-exercise"]; ok {
		t.Error("an unknown slug must not resolve")
	}
	if len(found) != 1 {
		t.Errorf("got %d resolved, want 1 (the repeated slug differs only in case)", len(found))
	}
}

func TestGetBySlugReportsNotFound(t *testing.T) {
	t.Parallel()

	_, err := newService(t).GetBySlug(context.Background(), "not-a-real-exercise")
	if !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func contains(haystack []string, needle string) bool {
	for _, value := range haystack {
		if value == needle {
			return true
		}
	}
	return false
}

// Paging must cover the catalog exactly once: every row reachable, none twice.
//
// The browse page shows PageSize of 455 rows, so a gap or a repeat is invisible
// on any single page and only shows up by walking the whole list. Before paging
// existed the page rendered the first 60 and there was no way to reach the rest.
func TestPagingWalksTheWholeCatalogWithoutGapsOrRepeats(t *testing.T) {
	t.Parallel()

	svc := newService(t)
	ctx := context.Background()

	_, total, err := svc.Search(ctx, exercises.Filter{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if total < 400 {
		t.Fatalf("the catalog looks unseeded or unmigrated: %d rows", total)
	}

	seen := map[string]bool{}
	pages := 0
	for offset := 0; offset < total; offset += exercises.PageSize {
		found, pageTotal, err := svc.Search(ctx, exercises.Filter{
			Limit:  exercises.PageSize,
			Offset: offset,
		})
		if err != nil {
			t.Fatalf("page at offset %d: %v", offset, err)
		}
		pages++

		// The total must not drift between pages, or the page links would
		// renumber themselves as someone walks the list.
		if pageTotal != total {
			t.Fatalf("total changed from %d to %d at offset %d", total, pageTotal, offset)
		}
		if len(found) == 0 {
			t.Fatalf("offset %d returned nothing while %d rows remain", offset, total-offset)
		}
		for _, e := range found {
			if seen[e.Slug] {
				t.Errorf("%q appeared on more than one page", e.Slug)
			}
			seen[e.Slug] = true
		}
	}

	if len(seen) != total {
		t.Errorf("walked %d distinct exercises across %d pages, want all %d", len(seen), pages, total)
	}
}

// An offset past the end is a page number someone typed. It must come back
// empty rather than erroring, so the handler can clamp and re-run.
func TestAnOffsetPastTheEndReturnsNothingRatherThanFailing(t *testing.T) {
	t.Parallel()

	svc := newService(t)
	found, total, err := svc.Search(context.Background(), exercises.Filter{Limit: 10, Offset: 100000})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("got %d rows past the end of the catalog", len(found))
	}
	if total == 0 {
		t.Error("total should still count every match, not the empty page")
	}
}

// A filtered page must be counted against the filter, not the whole catalog,
// or the page links would offer pages that render nothing.
func TestTheTotalTracksTheFilterNotTheCatalog(t *testing.T) {
	t.Parallel()

	svc := newService(t)
	ctx := context.Background()

	_, all, err := svc.Search(ctx, exercises.Filter{})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	_, filtered, err := svc.Search(ctx, exercises.Filter{Muscle: "chest"})
	if err != nil {
		t.Fatalf("filtered search: %v", err)
	}

	if filtered == 0 {
		t.Fatal("no exercises train the chest, which cannot be right")
	}
	if filtered >= all {
		t.Errorf("filtered total %d is not smaller than the catalog's %d", filtered, all)
	}
}
