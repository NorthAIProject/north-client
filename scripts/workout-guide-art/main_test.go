package main

import (
	"path/filepath"
	"testing"

	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

// The mapping tables are the whole point of this script — the generated SQL and
// the files on disk are just their output. Same posture as
// scripts/fitme-exercises: a muscle key outside the canonical set produces a
// catalog row the viewer silently ignores, which looks exactly like an exercise
// that trains nothing.

func TestEveryMappedMuscleIsCanonical(t *testing.T) {
	t.Parallel()

	for source, key := range muscleMap {
		if key == "" {
			continue // Cardio and Mobility are dropped on purpose
		}
		if !plan.IsMuscleGroup(key) {
			t.Errorf("muscleMap[%q] = %q, which is not in plan.MuscleGroups", source, key)
		}
	}
}

// Cardio and Mobility are training qualities, not muscles. They are in the map
// with an empty value rather than absent from it, so that resolve() reports
// them as a deliberate drop instead of an unmapped vocabulary word someone
// needs to go and fix.
func TestTrainingQualitiesAreMappedToNothingRatherThanOmitted(t *testing.T) {
	t.Parallel()

	for _, quality := range []string{"Cardio", "Mobility"} {
		key, ok := muscleMap[quality]
		if !ok {
			t.Errorf("muscleMap has no entry for %q; it should map to \"\", not be absent", quality)
			continue
		}
		if key != "" {
			t.Errorf("muscleMap[%q] = %q, want \"\" — it is not a muscle", quality, key)
		}
	}
}

// The catalog's equipment values feed the browse filter and the plan
// generator's candidate list, and are checked against the same vocabulary the
// plan validator uses. A value outside it means the validator could reject an
// exercise the catalog had just recommended.
func TestEveryMappedEquipmentIsRecognisedByThePlanValidator(t *testing.T) {
	t.Parallel()

	recognised := map[string]bool{"none": true, "other": true}
	for _, name := range plan.EquipmentNames() {
		recognised[name] = true
	}

	for source, value := range equipmentMap {
		if !recognised[value] {
			t.Errorf("equipmentMap[%q] = %q, which plan.EquipmentNames() does not carry", source, value)
		}
	}
}

// An alias pointing at a slug the catalog does not have imports nothing, and
// does it quietly: the entry still reads like curated data. This test is the
// reason readSeedSlugs parses the committed seed rather than querying a
// database — it runs without one.
func TestEveryAliasTargetExistsInTheCatalogSeed(t *testing.T) {
	t.Parallel()

	slugs, err := readSeedSlugs(filepath.Join("..", "..", "migrations", "00024_seed_exercises.sql"))
	if err != nil {
		t.Fatalf("reading the catalog seed: %v", err)
	}

	for upstream, target := range aliasMap {
		if !slugs[target] {
			t.Errorf("aliasMap[%q] = %q, which is not a slug in the catalog seed", upstream, target)
		}
	}
}

// An alias whose key already names a catalog row would never fire: resolve()
// checks for an exact match first. That is a silent no-op, and it means
// whoever added it believed they had folded two rows together when they had
// not.
func TestNoAliasIsShadowedByAnExactMatch(t *testing.T) {
	t.Parallel()

	slugs, err := readSeedSlugs(filepath.Join("..", "..", "migrations", "00024_seed_exercises.sql"))
	if err != nil {
		t.Fatalf("reading the catalog seed: %v", err)
	}

	for upstream := range aliasMap {
		if slugs[upstream] {
			t.Errorf("aliasMap[%q] is dead: %q already names a catalog row, so the exact match wins", upstream, upstream)
		}
	}
}

// Every upstream exercise must land in exactly one outcome. One that fell
// through every branch would leave three frames on disk that no catalog row
// points at — indistinguishable, from the page, from the import never having
// run.
func TestEveryEntryResolvesToExactlyOneOutcome(t *testing.T) {
	t.Parallel()

	catalog := map[string]bool{"elbow-plank": true, "pull-up": true}
	entries := []manifestEntry{
		{Slug: "pull-up", Name: "Pull-Up", Equipment: "Pull-up Bar", PrimaryMuscle: "Lats"},
		{Slug: "plank", Name: "Plank", Equipment: "Bodyweight", PrimaryMuscle: "Core"},
		{Slug: "ab-wheel", Name: "Ab Wheel", Equipment: "Bodyweight", PrimaryMuscle: "Core"},
	}

	resolved, report := resolve(entries, catalog)

	if len(resolved) != len(entries) {
		t.Fatalf("resolved %d entries, want %d", len(resolved), len(entries))
	}
	if report.exact != 1 || report.alias != 1 || report.added != 1 {
		t.Errorf("got exact=%d alias=%d added=%d, want 1/1/1", report.exact, report.alias, report.added)
	}

	byUpstream := map[string]resolution{}
	for _, r := range resolved {
		byUpstream[r.Upstream] = r
	}

	// The alias case is the one worth asserting on: the artwork directory keeps
	// upstream's name while the row it points at keeps North's.
	if got := byUpstream["plank"]; got.Outcome != outcomeAlias || got.CatalogSlug != "elbow-plank" || got.Upstream != "plank" {
		t.Errorf("plank resolved to %+v, want an alias onto elbow-plank keeping the upstream asset name", got)
	}
	if got := byUpstream["ab-wheel"]; got.Outcome != outcomeNew || got.CatalogSlug != "ab-wheel" {
		t.Errorf("ab-wheel resolved to %+v, want a new catalog row", got)
	}
}

// A muscle appearing as both primary and secondary would be highlighted twice
// by the viewer and read as a data error in the catalog.
func TestSecondaryMusclesNeverRepeatThePrimary(t *testing.T) {
	t.Parallel()

	entries := []manifestEntry{{
		Slug:             "hip-hinge",
		Name:             "Hip Hinge",
		Equipment:        "Bodyweight",
		PrimaryMuscle:    "Hamstrings",
		SecondaryMuscles: []string{"Posterior Chain", "Glutes"}, // both map onto hamstrings/glutes
	}}

	resolved, _ := resolve(entries, map[string]bool{})
	if len(resolved) != 1 {
		t.Fatalf("resolved %d entries, want 1", len(resolved))
	}

	for _, secondary := range resolved[0].Secondary {
		if contains(resolved[0].Primary, secondary) {
			t.Errorf("%q is both a primary and a secondary muscle", secondary)
		}
	}
}

// The validator reads the exercise name and overrules the source's equipment
// claim. Upstream files the dumbbell sumo deadlift under Dumbbell; the plan
// checker reads "deadlift" as a barbell, and a catalog that disagrees with the
// checker recommends gear the checker then rejects.
func TestThePlanValidatorOverrulesTheSourcesEquipmentClaim(t *testing.T) {
	t.Parallel()

	report := resolveReport{
		unmappedGear:       map[string]int{},
		equipmentOverrides: map[string]int{},
	}
	got := equipmentFor(manifestEntry{Name: "Dumbbell Sumo Deadlift", Equipment: "Dumbbell"}, &report)

	if got != plan.InferEquipment("Dumbbell Sumo Deadlift", "dumbbell") {
		t.Errorf("equipmentFor returned %q, which is not what plan.InferEquipment concluded", got)
	}
}

// Upstream ships no difficulty and no instructions, so a stretch has to be
// recognised from isStretch rather than from a category the source never
// carried.
func TestCategoryReadsIsStretchAndCardioRatherThanGuessing(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		entry manifestEntry
		want  string
	}{
		{"stretch", manifestEntry{IsStretch: true, ExerciseType: "duration"}, "stretching"},
		{"cardio by equipment", manifestEntry{Equipment: "Cardio", ExerciseType: "duration"}, "cardio"},
		{"cardio by type", manifestEntry{ExerciseType: "distance_duration"}, "cardio"},
		{"everything else", manifestEntry{ExerciseType: "weight_reps"}, "strength"},
	}

	for _, c := range cases {
		if got := category(c.entry); got != c.want {
			t.Errorf("%s: category = %q, want %q", c.name, got, c.want)
		}
	}
}

// Two upstream slugs resolving to one catalog row is silent data loss: both
// UPDATE the same row, the second wins, and the first's artwork sits on disk
// with nothing pointing at it.
//
// This is not hypothetical. aliasMap once folded "superman-hold" onto the
// catalog's "superman" while upstream also shipped its own "superman", which
// exact-matched the same row — 302 asset directories, 301 rows referencing
// them, and no error anywhere. TestNoAliasIsShadowedByAnExactMatch does not
// catch it: there the alias *key* is fine, and it is the *target* that is
// already claimed.
func TestNoTwoUpstreamSlugsClaimTheSameCatalogRow(t *testing.T) {
	t.Parallel()

	slugs, err := readSeedSlugs(filepath.Join("..", "..", "migrations", "00024_seed_exercises.sql"))
	if err != nil {
		t.Fatalf("reading the catalog seed: %v", err)
	}

	// An alias target that is itself a catalog slug will also be claimed by an
	// upstream exercise of that name, if upstream ships one. The script cannot
	// see upstream's slug list from here, so the rule is the stricter one: an
	// alias must not point at a row that an identically-named upstream
	// exercise could exact-match.
	claimed := map[string]string{}
	for upstream, target := range aliasMap {
		if !slugs[target] {
			continue // already reported by TestEveryAliasTargetExistsInTheCatalogSeed
		}
		if previous, taken := claimed[target]; taken {
			t.Errorf("aliasMap points both %q and %q at %q; the second UPDATE would overwrite the first", previous, upstream, target)
		}
		claimed[target] = upstream
	}
}

// The end-to-end version of the rule above, run against a manifest that
// reproduces the superman collision exactly.
func TestAnAliasNeverStealsARowFromAnExactMatch(t *testing.T) {
	t.Parallel()

	catalog := map[string]bool{"superman": true}
	entries := []manifestEntry{
		{Slug: "superman", Name: "Superman", Equipment: "Bodyweight", PrimaryMuscle: "Lower Back"},
		{Slug: "superman-hold", Name: "Superman Hold", Equipment: "Bodyweight", PrimaryMuscle: "Lower Back"},
	}

	resolved, _ := resolve(entries, catalog)

	rows := map[string]int{}
	for _, r := range resolved {
		rows[r.CatalogSlug]++
	}
	for slug, count := range rows {
		if count > 1 {
			t.Errorf("%d upstream exercises resolve to catalog row %q; one of their illustrations would be orphaned", count, slug)
		}
	}
}

// The curated primaries are the one place this script asserts anatomy rather
// than translating it, so they get the same check the translated keys get.
func TestEveryCuratedPrimaryIsCanonical(t *testing.T) {
	t.Parallel()

	for slug, keys := range mobilityPrimaries {
		if len(keys) == 0 {
			t.Errorf("mobilityPrimaries[%q] is empty, which defeats its purpose", slug)
		}
		for _, key := range keys {
			if !plan.IsMuscleGroup(key) {
				t.Errorf("mobilityPrimaries[%q] contains %q, which is not in plan.MuscleGroups", slug, key)
			}
		}
	}
}

// A new catalog row with no primary muscle highlights nothing on the viewer and
// breaks internal/exercises' TestSeededCatalogIsPresentAndCoherent. main()
// refuses to write one; this pins the resolve()-level behaviour that detection
// depends on.
func TestARowWithNoMappableMuscleIsReported(t *testing.T) {
	t.Parallel()

	entries := []manifestEntry{
		{Slug: "some-flow", Name: "Some Flow", Equipment: "Bodyweight", PrimaryMuscle: "Mobility"},
		{Slug: "cat-cow-stretch", Name: "Cat-Cow Stretch", Equipment: "Bodyweight", PrimaryMuscle: "Mobility"},
	}

	resolved, report := resolve(entries, map[string]bool{})

	if len(report.withoutPrimaries) != 1 || report.withoutPrimaries[0] != "some-flow" {
		t.Errorf("withoutPrimaries = %v, want exactly [some-flow]", report.withoutPrimaries)
	}

	// The curated one must come out with its hand-assigned key, not empty.
	for _, r := range resolved {
		if r.Upstream == "cat-cow-stretch" && len(r.Primary) == 0 {
			t.Error("cat-cow-stretch has no primary muscle despite an entry in mobilityPrimaries")
		}
	}
}
