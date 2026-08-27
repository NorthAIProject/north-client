// Command workout-guide-art imports the Workout Guide pose artwork into
// North: it writes the SVG frames into web/assets/exercises and generates the
// migration that points catalog rows at them.
//
// It is a one-off, but it is committed alongside its output for the same
// reason scripts/fitme-exercises is — the output is hundreds of rows of
// derived data, and the interesting part is the decisions above it. Here that
// is aliasMap: upstream names movements tersely ("bench-press") where North's
// catalog, seeded from FitMe, names them verbosely
// ("barbell-bench-press-medium-grip"). Only 22 of 302 slugs match outright.
// Whether "bench-press" was folded into an existing row or added as a second
// one cannot be checked by reading the generated SQL.
//
// Run it from the repository root, against a shallow clone:
//
//	git clone --depth 1 https://github.com/bryllim/workout-guide /tmp/workout-guide
//	go run ./scripts/workout-guide-art \
//	  -source /tmp/workout-guide \
//	  -assets web/assets/exercises \
//	  -out    migrations/20260827150000_exercise_illustrations.sql
//
// The upstream repository is never vendored: it carries 906 PNG duplicates of
// the SVGs and an npm workspace, none of which belongs in this tree.
//
// # Licensing
//
// The artwork is CC BY-SA 4.0 (upstream's LICENSE-ASSETS — its *code* is MIT,
// which is not the file that governs here), tracing back to Everkinetic.
// Commercial use is granted; credit, a licence link, a statement of changes,
// and ShareAlike on adaptations are required.
// The frames are not edited — see writeFrames — so the changes to declare are
// only the packaging ones. They are recorded in the NOTICE file this command
// writes beside the assets. Do not drop that file.
package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

func main() {
	source := flag.String("source", "", "path to a workout-guide checkout")
	assets := flag.String("assets", "web/assets/exercises", "directory to write the SVG frames into")
	out := flag.String("out", "", "path to write the generated migration to")
	seed := flag.String("seed", "migrations/00024_seed_exercises.sql", "the catalog seed, read to resolve alias targets")
	flag.Parse()

	if *source == "" || *out == "" {
		log.Fatal("both -source and -out are required")
	}

	manifestPath := filepath.Join(*source, "packages", "workout-guide", "manifest.json")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		log.Fatalf("reading the upstream manifest: %v", err)
	}

	var entries []manifestEntry
	if err = json.Unmarshal(raw, &entries); err != nil {
		log.Fatalf("parsing the upstream manifest: %v", err)
	}
	log.Printf("parsed %d upstream exercises", len(entries))

	catalog, err := readSeedSlugs(*seed)
	if err != nil {
		log.Fatalf("reading the catalog seed: %v", err)
	}
	log.Printf("catalog seed carries %d slugs", len(catalog))

	resolved, report := resolve(entries, catalog)
	report.log()

	// A catalog row with no primary muscle highlights nothing on the 3D viewer
	// and fails internal/exercises' TestSeededCatalogIsPresentAndCoherent. That
	// is a broken catalog rather than a lossy import, so it stops the run here
	// instead of being written out and discovered by a test hours later.
	if len(report.withoutPrimaries) > 0 {
		log.Fatalf("these upstream exercises have no muscle North can map, and no entry in mobilityPrimaries: %v",
			report.withoutPrimaries)
	}

	written, err := writeFrames(*source, *assets, entries)
	if err != nil {
		log.Fatalf("writing the frames: %v", err)
	}
	log.Printf("wrote %d gzipped frames to %s", written, *assets)

	if err := os.WriteFile(filepath.Join(*assets, "NOTICE"), []byte(notice), 0o644); err != nil {
		log.Fatalf("writing the NOTICE: %v", err)
	}

	if err := os.WriteFile(*out, []byte(render(resolved)), 0o644); err != nil {
		log.Fatalf("writing the migration: %v", err)
	}
	log.Printf("wrote %s", *out)
}

// ---------------------------------------------------------------------------
// Upstream shape
// ---------------------------------------------------------------------------

// manifestEntry is one exercise in upstream's manifest.json. Only the fields
// North uses are declared; attribution travels in the NOTICE file instead,
// because it is identical for every frame bar the 76 with a traced source.
type manifestEntry struct {
	Slug             string   `json:"slug"`
	Name             string   `json:"name"`
	Equipment        string   `json:"equipment"`
	ExerciseType     string   `json:"exerciseType"`
	PrimaryMuscle    string   `json:"primaryMuscle"`
	SecondaryMuscles []string `json:"secondaryMuscles"`
	IsStretch        bool     `json:"isStretch"`
	Frames           []struct {
		Index int    `json:"index"`
		Path  string `json:"path"`
	} `json:"frames"`
}

// ---------------------------------------------------------------------------
// Mapping
// ---------------------------------------------------------------------------

// aliasMap folds an upstream slug into a catalog row that already describes the
// same movement under a different name.
//
// Deliberately short. Fuzzy slug matching proposes 68 pairs and most are wrong
// — it offers "v-up" for "v-bar-pull-up" and "seated-leg-curl" for
// "seated-finger-curl" — so every entry here was confirmed against both rows'
// equipment, which is the field that separates a renaming from a variation.
// The pairs it rejects are rejected for a reason worth keeping:
//
//   - incline-bench-press (Barbell) is not incline-dumbbell-bench-press
//   - hack-squat (Machine) is not barbell-hack-squat
//   - forward-lunge (Bodyweight) is not barbell-forward-lunge
//   - romanian-deadlift is not romanian-deadlift-from-deficit
//   - superman-hold is not superman: upstream ships both, and folding the hold
//     onto the catalog's superman left two upstream slugs fighting over one
//     row. TestNoTwoUpstreamSlugsClaimTheSameCatalogRow now refuses that.
//
// Those are separate movements and each keeps its own row. The failure this
// caution avoids is the loud one: a wrong alias prints one exercise's artwork
// onto another's page. A missing alias only leaves two similar rows, one of
// which has no picture.
var aliasMap = map[string]string{
	"bench-press":             "barbell-bench-press-medium-grip",
	"close-grip-lat-pulldown": "close-grip-front-lat-pulldown",
	"deadlift":                "barbell-deadlift",
	"donkey-calf-raise":       "weighted-donkey-calf-raise",
	"dumbbell-shrug":          "standing-dumbbell-shrug",
	"elliptical":              "elliptical-trainer",
	"hip-thrust":              "barbell-hip-thrust",
	"incline-dumbbell-press":  "incline-dumbbell-bench-press",
	"plank":                   "elbow-plank",
	"shrug":                   "barbell-shrug",
	"squat":                   "barbell-full-squat",
}

// muscleMap translates upstream's muscle vocabulary into North's canonical
// keys (internal/workouts/plan.MuscleGroups).
//
// The lossy entries, all in the same direction — a region name collapsing onto
// the one muscle in it the 3D model can colour:
//
//   - Back and Lats both become lats. Upstream uses "Back" for pulls it does
//     not care to place more precisely; lats is the honest single answer.
//   - Upper Back becomes rhomboids, matching how fitme-exercises read
//     middle_back. Traps keep their own key for movements that name them.
//   - Legs becomes quads and Posterior Chain becomes hamstrings: the prime
//     mover in each, with the rest of the region arriving as secondaries.
//   - Hips becomes glutes, for the same reason abductors did in the FitMe
//     import — two keys claiming one mesh helps nobody.
//
// Cardio and Mobility map to nothing on purpose. They are training qualities,
// not muscles, and inventing anatomy for them would put a colour on the model
// that the exercise does not earn. Rows whose only muscle is one of these end
// up with an empty primary_muscles, which is what the catalog already does for
// movements it cannot place.
var muscleMap = map[string]string{
	"Core":            "abs",
	"Glutes":          "glutes",
	"Quads":           "quads",
	"Chest":           "chest",
	"Shoulders":       "delts",
	"Rear Delts":      "delts",
	"Back":            "lats",
	"Lats":            "lats",
	"Upper Back":      "rhomboids",
	"Lower Back":      "erectors",
	"Triceps":         "triceps",
	"Biceps":          "biceps",
	"Forearms":        "forearms",
	"Grip":            "forearms",
	"Hamstrings":      "hamstrings",
	"Posterior Chain": "hamstrings",
	"Legs":            "quads",
	"Calves":          "calves",
	"Adductors":       "adductors",
	"Groin":           "adductors",
	"Hips":            "glutes",
	"Traps":           "traps",

	"Cardio":   "", // a quality, not a muscle
	"Mobility": "",
}

// mobilityPrimaries hand-assigns a primary muscle to the movements whose only
// muscle upstream names is "Mobility".
//
// Every catalog row must carry a primary muscle: the browse page and the 3D
// viewer both read it, and internal/exercises' TestSeededCatalogIsPresentAndCoherent
// enforces it. "Mobility" is a training quality, so mapping it in muscleMap
// would put invented anatomy on 14 rows. Naming the four affected movements
// individually is the same trade fitme-exercises made with its curated
// secondaries: a short hand-reviewed list beats a rule that guesses.
//
// Each is the joint the stretch actually moves, kept to one key rather than
// the whole region it loosens.
var mobilityPrimaries = map[string][]string{
	"cat-cow-stretch":         {"erectors"},   // segmental spinal flexion and extension
	"torso-twist-stretch":     {"abs"},        // rotation through the trunk
	"leg-swings-stretch":      {"hamstrings"}, // dynamic hip flexion and extension
	"worlds-greatest-stretch": {"adductors"},  // the lunge's groin opening is the point of it
}

// equipmentMap translates upstream's equipment vocabulary into the words
// internal/workouts/plan.EquipmentNames matches on.
//
// It is only the starting point: plan.InferEquipment reads the exercise name
// and wins where it has an opinion, exactly as in the FitMe import. Upstream
// files "Dumbbell Sumo Deadlift" under Dumbbell and the validator reads
// "deadlift" as a barbell — deferring to the validator is what keeps the
// catalog from recommending gear the plan checker will then reject.
//
// The long tail (Wall, Towel, Doorway, Chair, Box, Plate, Stability Ball) all
// becomes "other" rather than "none": each needs a physical object, and
// claiming otherwise puts them in front of someone training in a hotel room.
var equipmentMap = map[string]string{
	"Bodyweight":      "none",
	"Dumbbell":        "dumbbell",
	"Kettlebell":      "kettlebell",
	"Barbell":         "barbell",
	"Machine":         "machine",
	"Cable":           "machine",
	"Resistance Band": "resistance band",
	"Pull-up Bar":     "pull-up bar",
	"Bench":           "bench",
	"Cardio":          "other",
	"Wall":            "other",
	"Towel":           "other",
	"Doorway":         "other",
	"Chair":           "other",
	"Box":             "other",
	"Plate":           "other",
	"Stability Ball":  "other",
}

// ---------------------------------------------------------------------------
// Resolution
// ---------------------------------------------------------------------------

// outcome is what happened to one upstream exercise. Every upstream slug gets
// exactly one, and the test asserts as much: an exercise that fell through
// every branch would leave artwork on disk that no catalog row points at,
// which looks from the outside like nothing happened at all.
type outcome int

const (
	outcomeExact outcome = iota // upstream slug already names a catalog row
	outcomeAlias                // aliasMap folds it into a differently-named row
	outcomeNew                  // no catalog equivalent; insert a row
)

type resolution struct {
	Outcome     outcome
	CatalogSlug string // the row to point at art
	Upstream    string // the asset directory name

	// Set for outcomeNew only.
	Name      string
	Category  string
	Equipment string
	Primary   []string
	Secondary []string
}

type resolveReport struct {
	exact, alias, added int
	unmappedMuscles     map[string]int
	unmappedGear        map[string]int
	droppedQualities    map[string]int
	withoutPrimaries    []string
	orphanedAliases     []string
	curatedPrimaries    int
	equipmentOverrides  map[string]int
}

func (r resolveReport) log() {
	log.Printf("%d matched by slug, %d matched through aliasMap, %d new catalog rows", r.exact, r.alias, r.added)

	for muscle, count := range r.unmappedMuscles {
		log.Printf("WARNING: no muscle mapping for %q (%d rows) — add it to muscleMap", muscle, count)
	}
	for gear, count := range r.unmappedGear {
		log.Printf("WARNING: no equipment mapping for %q (%d rows) — defaulted to \"other\"", gear, count)
	}
	for quality, count := range r.droppedQualities {
		log.Printf("dropped %q as a muscle on %d rows (it is a training quality)", quality, count)
	}
	if r.curatedPrimaries > 0 {
		log.Printf("%d rows took a hand-curated primary muscle from mobilityPrimaries", r.curatedPrimaries)
	}
	// An alias whose target does not exist is the quietest failure available:
	// it reads like curated data and silently imports nothing.
	for _, slug := range r.orphanedAliases {
		log.Printf("WARNING: aliasMap points at %q, which is not in the catalog seed — fix it or drop the entry", slug)
	}
	for change, count := range r.equipmentOverrides {
		log.Printf("corrected equipment %s on %d rows (the validator's rules win over the source)", change, count)
	}
}

func resolve(entries []manifestEntry, catalog map[string]bool) ([]resolution, resolveReport) {
	report := resolveReport{
		unmappedMuscles:    map[string]int{},
		unmappedGear:       map[string]int{},
		droppedQualities:   map[string]int{},
		equipmentOverrides: map[string]int{},
	}

	for upstream, target := range aliasMap {
		if !catalog[target] {
			report.orphanedAliases = append(report.orphanedAliases, fmt.Sprintf("%s -> %s", upstream, target))
		}
	}
	sort.Strings(report.orphanedAliases)

	var resolved []resolution
	for _, e := range entries {
		switch {
		case catalog[e.Slug]:
			report.exact++
			resolved = append(resolved, resolution{Outcome: outcomeExact, CatalogSlug: e.Slug, Upstream: e.Slug})

		case aliasMap[e.Slug] != "" && catalog[aliasMap[e.Slug]]:
			report.alias++
			resolved = append(resolved, resolution{Outcome: outcomeAlias, CatalogSlug: aliasMap[e.Slug], Upstream: e.Slug})

		default:
			report.added++
			row := resolution{
				Outcome:     outcomeNew,
				CatalogSlug: e.Slug,
				Upstream:    e.Slug,
				Name:        e.Name,
				Category:    category(e),
				Equipment:   equipmentFor(e, &report),
				Primary:     muscles([]string{e.PrimaryMuscle}, &report),
			}
			if len(row.Primary) == 0 {
				row.Primary = mobilityPrimaries[e.Slug]
				if len(row.Primary) > 0 {
					report.curatedPrimaries++
				}
			}
			row.Secondary = removeAll(muscles(e.SecondaryMuscles, &report), row.Primary)
			if len(row.Primary) == 0 {
				report.withoutPrimaries = append(report.withoutPrimaries, e.Slug)
			}
			resolved = append(resolved, row)
		}
	}

	sort.Slice(resolved, func(i, j int) bool { return resolved[i].Upstream < resolved[j].Upstream })
	return resolved, report
}

// category maps upstream's exerciseType onto the catalog's vocabulary.
//
// Upstream carries no difficulty and no instructions, so new rows take the
// column defaults rather than a guess. A fabricated "intermediate" on 268 rows
// would be indistinguishable from a curated one.
func category(e manifestEntry) string {
	switch {
	case e.IsStretch:
		return "stretching"
	case e.ExerciseType == "distance_duration", e.Equipment == "Cardio":
		return "cardio"
	default:
		return "strength"
	}
}

func equipmentFor(e manifestEntry, report *resolveReport) string {
	claimed, ok := equipmentMap[e.Equipment]
	if !ok {
		report.unmappedGear[e.Equipment]++
		claimed = "other"
	}

	inferred := plan.InferEquipment(e.Name, claimed)
	if inferred != "" && inferred != claimed {
		report.equipmentOverrides[claimed+" -> "+inferred]++
		return inferred
	}
	return claimed
}

func muscles(sources []string, report *resolveReport) []string {
	var keys []string
	for _, source := range sources {
		if source == "" {
			continue
		}
		key, ok := muscleMap[source]
		if !ok {
			report.unmappedMuscles[source]++
			continue
		}
		if key == "" {
			report.droppedQualities[source]++
			continue
		}
		if !contains(keys, key) {
			keys = append(keys, key)
		}
	}
	return keys
}

// ---------------------------------------------------------------------------
// Frames
// ---------------------------------------------------------------------------

// The frames are stored byte-identical to upstream apart from gzip.
//
// They ship drawn white on transparent, which would be invisible on the light
// theme — but the template never loads them as images. web/shared/exerciseart
// uses each frame as a CSS mask and paints currentColor through it, so only
// the alpha channel is read and the fill inside the file is irrelevant. That
// is what lets one asset serve both themes without editing 906 files, and it
// keeps the list of changes CC BY-SA requires North to declare down to
// "compressed, and the PNGs were not imported".

// writeFrames stores each frame gzipped.
//
// The set is 24.7 MB of SVG raw and 10.0 MB gzipped, and it is embedded into
// the binary — so this is 15 MB off every build and every image layer. It also
// fixes the wire: cmd/web/main.go mounts no compression middleware, so raw
// frames would go out uncompressed at ~28 KB each. mountAssets serves these
// bytes verbatim under Content-Encoding: gzip.
func writeFrames(source, assets string, entries []manifestEntry) (int, error) {
	base := filepath.Join(source, "packages", "workout-guide")

	written := 0
	for _, e := range entries {
		dir := filepath.Join(assets, e.Slug)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return written, fmt.Errorf("creating %s: %w", dir, err)
		}

		for _, frame := range e.Frames {
			svg, err := os.ReadFile(filepath.Join(base, frame.Path))
			if err != nil {
				return written, fmt.Errorf("reading %s: %w", frame.Path, err)
			}

			var buf bytes.Buffer
			zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
			if err != nil {
				return written, fmt.Errorf("creating the gzip writer: %w", err)
			}
			if _, err := zw.Write(svg); err != nil {
				return written, fmt.Errorf("compressing %s: %w", frame.Path, err)
			}
			if err := zw.Close(); err != nil {
				return written, fmt.Errorf("flushing %s: %w", frame.Path, err)
			}

			name := filepath.Join(dir, fmt.Sprintf("frame-%d.svg.gz", frame.Index))
			if err := os.WriteFile(name, buf.Bytes(), 0o644); err != nil {
				return written, fmt.Errorf("writing %s: %w", name, err)
			}
			written++
		}
	}
	return written, nil
}

const notice = `Exercise illustrations
======================

Source:  https://github.com/bryllim/workout-guide
Artwork: Copyright (c) 2026 Bryl Lim  <https://bryllim.com>
Origin:  The original pose artwork comes from Everkinetic
         <https://github.com/everkinetic/data>
License: Creative Commons Attribution-ShareAlike 4.0 International
         https://creativecommons.org/licenses/by-sa/4.0/

Note that upstream's LICENSE covers its code and is MIT. The artwork in this
directory is governed by its LICENSE-ASSETS, which is CC BY-SA 4.0. Adaptations
must be shared under the same licence.

Changes made by North, as CC BY-SA requires them to be stated:

  - Each frame is stored gzipped and served with Content-Encoding: gzip.
  - Upstream's PNG copies of each frame were not imported.
  - The artwork itself is unmodified: not redrawn, recoloured, cropped, or
    rescaled. North paints it by using each frame as a CSS mask, which reads
    only its alpha channel, so the files did not need editing to work on both
    the light and dark themes.

Regenerate with scripts/workout-guide-art. Do not hand-edit these files.
`

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

func render(resolved []resolution) string {
	var b strings.Builder

	fmt.Fprintf(&b, `-- +goose Up
-- +goose StatementBegin

-- Generated by scripts/workout-guide-art on %s. Do not hand-edit: regenerate.
--
-- Source: github.com/bryllim/workout-guide, 302 exercises of pose artwork
-- under CC BY-SA 4.0. See web/assets/exercises/NOTICE for attribution and the
-- record of changes the licence requires.
--
-- illustration_slug names the directory under web/assets/exercises holding a
-- row's three frames. It is its own column rather than a reuse of slug because
-- the two vocabularies disagree: North's catalog came from FitMe and names the
-- bench press "barbell-bench-press-medium-grip", where upstream calls it
-- "bench-press". Only 22 of 302 slugs matched outright; aliasMap in the script
-- resolves a further 12 by hand, and the rest arrive as new rows below.
--
-- Rows with no artwork keep the empty default, which the template reads as
-- "render no illustration".

ALTER TABLE exercises ADD COLUMN illustration_slug text NOT NULL DEFAULT '';

`, time.Now().UTC().Format("2006-01-02"))

	b.WriteString("-- Existing catalog rows, matched by slug or through aliasMap.\n")
	for _, r := range resolved {
		if r.Outcome == outcomeNew {
			continue
		}
		note := ""
		if r.Outcome == outcomeAlias {
			note = fmt.Sprintf("  -- aliased from %q", r.Upstream)
		}
		fmt.Fprintf(&b, "UPDATE exercises SET illustration_slug = %s WHERE slug = %s;%s\n",
			quote(r.Upstream), quote(r.CatalogSlug), note)
	}

	b.WriteString(`
-- Movements upstream carries that the catalog did not.
--
-- difficulty and instructions take their column defaults: upstream ships
-- neither, and a fabricated difficulty on every one of these rows would be
-- indistinguishable from a curated one.
INSERT INTO exercises (slug, name, category, equipment, primary_muscles, secondary_muscles, illustration_slug) VALUES
`)

	var added []resolution
	for _, r := range resolved {
		if r.Outcome == outcomeNew {
			added = append(added, r)
		}
	}
	for i, r := range added {
		terminator := ","
		if i == len(added)-1 {
			terminator = ";"
		}
		fmt.Fprintf(&b, "    (%s, %s, %s, %s, %s, %s, %s)%s\n",
			quote(r.CatalogSlug), quote(r.Name), quote(r.Category), quote(r.Equipment),
			textArray(r.Primary), textArray(r.Secondary), quote(r.Upstream), terminator)
	}

	b.WriteString(`
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Only the rows this migration inserted. Deleting by illustration_slug would
-- also take the pre-existing rows that merely gained artwork.
DELETE FROM exercises WHERE slug IN (
`)
	for i, r := range added {
		terminator := ","
		if i == len(added)-1 {
			terminator = ""
		}
		fmt.Fprintf(&b, "    %s%s\n", quote(r.CatalogSlug), terminator)
	}
	b.WriteString(`);

ALTER TABLE exercises DROP COLUMN illustration_slug;

-- +goose StatementEnd
`)

	return b.String()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// seedSlug matches the leading slug of each tuple in the catalog seed.
//
// Reading the committed seed rather than the live database keeps this command
// runnable without one, and keeps the alias check honest about what a fresh
// migration run would actually produce.
var seedSlug = regexp.MustCompile(`(?m)^\s*\('([a-z0-9-]+)',`)

func readSeedSlugs(path string) (map[string]bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	slugs := map[string]bool{}
	for _, match := range seedSlug.FindAllStringSubmatch(string(raw), -1) {
		slugs[match[1]] = true
	}
	if len(slugs) == 0 {
		return nil, fmt.Errorf("found no slugs in %s — is this the right file?", path)
	}
	return slugs, nil
}

func removeAll(from, unwanted []string) []string {
	var kept []string
	for _, value := range from {
		if !contains(unwanted, value) {
			kept = append(kept, value)
		}
	}
	return kept
}

func contains(haystack []string, needle string) bool {
	for _, value := range haystack {
		if value == needle {
			return true
		}
	}
	return false
}

func quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func textArray(values []string) string {
	if len(values) == 0 {
		return "'{}'"
	}
	return "'{" + strings.Join(values, ",") + "}'"
}
