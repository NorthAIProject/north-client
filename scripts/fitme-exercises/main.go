// Command fitme-exercises converts the FitMe project's exercise seed into a
// goose migration for North's exercises table.
//
// It is a one-off, but it is committed alongside its output because the output
// is 300 rows of derived data: the mapping decisions below (which FitMe muscle
// becomes which North key, what counts as a barbell) are the interesting part,
// and a reviewer reading only the generated SQL cannot check them.
//
// Run it from the repository root:
//
//	go run ./scripts/fitme-exercises \
//	  -in  ../../Projects/FitME/fitme-grpc/internal/migrations/010_insert_exercises.sql \
//	  -out migrations/00024_seed_exercises.sql
//
// The input lives outside this repository, so regenerating requires a FitMe
// checkout. That is deliberate: vendoring 2,700 lines of someone else's seed
// file to regenerate a file that is already committed would be worse.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

func main() {
	in := flag.String("in", "", "path to FitMe's 010_insert_exercises.sql")
	out := flag.String("out", "", "path to write the generated migration to")
	flag.Parse()

	if *in == "" || *out == "" {
		log.Fatal("both -in and -out are required")
	}

	source, err := os.ReadFile(*in)
	if err != nil {
		log.Fatalf("reading the FitMe seed: %v", err)
	}

	rows, err := parse(string(source))
	if err != nil {
		log.Fatalf("parsing the FitMe seed: %v", err)
	}
	log.Printf("parsed %d rows", len(rows))

	exercises, report := convert(rows)
	report.log()

	if err := os.WriteFile(*out, []byte(render(exercises)), 0o644); err != nil {
		log.Fatalf("writing the migration: %v", err)
	}
	log.Printf("wrote %d exercises to %s", len(exercises), *out)
}

// ---------------------------------------------------------------------------
// Parsing
// ---------------------------------------------------------------------------

// row is one tuple of FitMe's INSERT, in the order its column list declares.
// Unlike the food seed (see scripts/fitme-ingredients), this file's declared
// column order and its value order actually agree.
type row struct {
	Name         string
	Type         string
	Muscle       string
	Equipment    string
	Difficulty   string
	Instructions string
	Video        string
}

const fieldsPerRow = 7

// parse pulls the string literals out of each `(gen_random_uuid(), '...', ...)`
// tuple. A hand-rolled scanner rather than a SQL parser: the input is one
// machine-generated INSERT with a fixed shape, and the only subtlety is SQL's
// doubled-quote escape, which is four lines below.
func parse(source string) ([]row, error) {
	chunks := strings.Split(source, "(gen_random_uuid(),")
	if len(chunks) < 2 {
		return nil, fmt.Errorf("found no `(gen_random_uuid(),` tuples — is this the right file?")
	}

	var rows []row
	for i, chunk := range chunks[1:] {
		values := stringLiterals(chunk)
		if len(values) != fieldsPerRow {
			return nil, fmt.Errorf("tuple %d has %d string fields, want %d: %.120q", i+1, len(values), fieldsPerRow, chunk)
		}
		rows = append(rows, row{
			Name:         values[0],
			Type:         values[1],
			Muscle:       values[2],
			Equipment:    values[3],
			Difficulty:   values[4],
			Instructions: values[5],
			Video:        values[6],
		})
	}
	return rows, nil
}

// stringLiterals reads single-quoted literals until the tuple's closing paren,
// unescaping SQL's doubled quote (” -> ').
func stringLiterals(chunk string) []string {
	var values []string

	for i := 0; i < len(chunk); i++ {
		switch chunk[i] {
		case ')':
			// End of this tuple. Anything after it belongs to the next one,
			// which Split has already handed us separately.
			return values
		case '\'':
			var b strings.Builder
			j := i + 1
			for j < len(chunk) {
				if chunk[j] != '\'' {
					b.WriteByte(chunk[j])
					j++
					continue
				}
				if j+1 < len(chunk) && chunk[j+1] == '\'' {
					b.WriteByte('\'')
					j += 2
					continue
				}
				break
			}
			values = append(values, b.String())
			i = j
		}
	}
	return values
}

// ---------------------------------------------------------------------------
// Mapping
// ---------------------------------------------------------------------------

// muscleMap translates FitMe's muscle vocabulary into North's canonical keys
// (internal/workouts/plan.MuscleGroups).
//
// Two entries are lossy on purpose:
//   - middle_back covers the rhomboids and the mid traps; rhomboids is the
//     closer single answer, and the traps have their own key for exercises
//     that actually name them.
//   - abductors folds into glutes, because the abductor that matters here is
//     gluteus medius/minimus, which the model already colours under glutes.
//     Giving abductors its own key would mean two keys claiming one mesh.
var muscleMap = map[string]string{
	"abdominals":  "abs",
	"abductors":   "glutes",
	"adductors":   "adductors",
	"biceps":      "biceps",
	"calves":      "calves",
	"chest":       "chest",
	"forearms":    "forearms",
	"glutes":      "glutes",
	"hamstrings":  "hamstrings",
	"lats":        "lats",
	"lower_back":  "erectors",
	"middle_back": "rhomboids",
	"neck":        "neck",
	"quadriceps":  "quads",
	"shoulders":   "delts",
	"traps":       "traps",
	"triceps":     "triceps",
}

// equipmentMap translates FitMe's equipment vocabulary into the words
// internal/workouts/plan.availableEquipment matches on, so the catalog filter
// and the plan validator cannot disagree about what someone owns.
//
// "other" stays "other" rather than collapsing to "none": it covers sleds,
// tires, ropes and plates, and claiming those need no equipment would put them
// in front of someone training in a bedroom.
var equipmentMap = map[string]string{
	"barbell":      "barbell",
	"body_only":    "none",
	"cable":        "machine", // plan's machine rule already matches "cable"
	"dumbbell":     "dumbbell",
	"e-z_curl_bar": "barbell",
	"foam_roll":    "other",
	"kettlebells":  "kettlebell",
	"machine":      "machine",
	"none":         "none",
	"other":        "other",
}

// secondaryMuscles is hand-curated, keyed by slug.
//
// FitMe carries exactly one muscle per exercise, so every secondary here is a
// judgement rather than a translation. Only the compound lifts get one: those
// are the movements where "what does this train" has an answer beyond the one
// muscle the source names, and where a lifter would notice the omission.
// Everything else seeds empty — an empty array is honest, and a guessed one
// puts wrong anatomy on a 3D model that exists to teach anatomy.
var secondaryMuscles = map[string][]string{
	"barbell-deadlift":                     {"glutes", "hamstrings", "erectors", "traps", "forearms", "quads"},
	"barbell-full-squat":                   {"glutes", "hamstrings", "erectors", "abs"},
	"olympic-squat":                        {"glutes", "hamstrings", "erectors", "abs"},
	"barbell-hack-squat":                   {"glutes", "hamstrings", "erectors"},
	"narrow-stance-squat":                  {"glutes", "erectors"},
	"overhead-squat":                       {"glutes", "delts", "erectors", "abs"},
	"kneeling-squat":                       {"glutes", "erectors"},
	"barbell-back-squat-to-box":            {"glutes", "hamstrings", "erectors"},
	"box-squat-with-bands":                 {"glutes", "hamstrings", "erectors"},
	"squat-with-chains":                    {"glutes", "hamstrings", "erectors"},
	"reverse-band-box-squat":               {"glutes", "hamstrings", "erectors"},
	"sumo-deadlift":                        {"glutes", "quads", "erectors", "traps", "forearms"},
	"clean-deadlift":                       {"glutes", "quads", "erectors", "traps"},
	"snatch-deadlift":                      {"glutes", "quads", "erectors", "traps"},
	"axle-deadlift":                        {"glutes", "erectors", "traps", "forearms"},
	"barbell-deficit-deadlift":             {"glutes", "quads", "erectors", "traps"},
	"deadlift-with-bands":                  {"glutes", "hamstrings", "erectors", "traps"},
	"deadlift-with-chains":                 {"glutes", "hamstrings", "erectors", "traps"},
	"romanian-deadlift-with-dumbbells":     {"glutes", "erectors", "forearms"},
	"romanian-deadlift-from-deficit":       {"glutes", "erectors", "forearms"},
	"single-arm-side-deadlift":             {"glutes", "erectors", "forearms", "abs"},
	"barbell-bench-press-medium-grip":      {"triceps", "delts"},
	"close-grip-bench-press":               {"triceps", "delts"},
	"dumbbell-bench-press":                 {"triceps", "delts"},
	"incline-dumbbell-bench-press":         {"triceps", "delts"},
	"dumbbell-floor-press":                 {"triceps", "delts"},
	"chest-dip":                            {"triceps", "delts"},
	"triceps-dip":                          {"chest", "delts"},
	"weighted-bench-dip":                   {"chest", "delts"},
	"pushups":                              {"triceps", "delts", "abs"},
	"push-ups-close-triceps-position":      {"chest", "delts"},
	"pull-up":                              {"biceps", "rhomboids", "forearms"},
	"pullups":                              {"biceps", "rhomboids", "forearms"},
	"weighted-pull-up":                     {"biceps", "rhomboids", "forearms"},
	"v-bar-pull-up":                        {"biceps", "rhomboids", "forearms"},
	"rocky-pull-ups-pulldowns":             {"biceps", "rhomboids", "forearms"},
	"close-grip-front-lat-pulldown":        {"biceps", "rhomboids"},
	"close-grip-pull-down":                 {"biceps", "rhomboids"},
	"seated-cable-rows":                    {"lats", "biceps", "erectors"},
	"t-bar-row":                            {"lats", "biceps", "erectors"},
	"t-bar-row-with-handle":                {"lats", "biceps", "erectors"},
	"one-arm-dumbbell-row":                 {"lats", "biceps", "erectors"},
	"one-arm-long-bar-row":                 {"lats", "biceps", "erectors"},
	"bent-over-two-arm-long-bar-row":       {"lats", "biceps", "erectors"},
	"reverse-grip-bent-over-row":           {"lats", "biceps", "erectors"},
	"incline-dumbbell-row":                 {"lats", "biceps"},
	"shotgun-row":                          {"lats", "biceps"},
	"standing-dumbbell-upright-row":        {"delts", "biceps"},
	"push-press":                           {"delts", "triceps", "quads", "abs"},
	"clean-and-press":                      {"delts", "traps", "quads", "glutes", "erectors"},
	"clean-and-jerk":                       {"delts", "traps", "quads", "glutes", "erectors"},
	"hang-clean":                           {"traps", "quads", "glutes", "erectors", "forearms"},
	"clean-from-blocks":                    {"traps", "quads", "glutes", "erectors"},
	"power-clean-from-blocks":              {"traps", "quads", "glutes", "erectors"},
	"power-snatch":                         {"traps", "delts", "quads", "glutes", "erectors"},
	"kettlebell-thruster":                  {"delts", "triceps", "glutes", "abs"},
	"kettlebell-sumo-deadlift-high-pull":   {"traps", "delts", "glutes", "erectors"},
	"barbell-forward-lunge":                {"glutes", "hamstrings", "abs"},
	"barbell-hip-thrust":                   {"hamstrings", "erectors"},
	"barbell-glute-bridge":                 {"hamstrings", "erectors"},
	"single-leg-press":                     {"glutes", "hamstrings"},
	"single-leg-squat-with-knee-tap":       {"glutes", "abs"},
	"single-arm-kettlebell-overhead-squat": {"delts", "glutes", "abs"},
	"burpee":                               {"chest", "triceps", "quads", "abs"},
}

// ---------------------------------------------------------------------------
// Conversion
// ---------------------------------------------------------------------------

type exercise struct {
	Slug         string
	Name         string
	Category     string
	Equipment    string
	Difficulty   string
	Instructions string
	Video        string
	Primary      []string
	Secondary    []string
}

type conversionReport struct {
	repeatedRows      int
	mergedMuscles     []string
	unmappedMuscles   map[string]int
	unmappedGear      map[string]int
	withSecondaries   int
	withoutPrimaries  int
	orphanedCurations []string
	// equipmentCorrections counts where the validator's reading of the name
	// overrode the source's equipment claim, keyed "from -> to".
	equipmentCorrections map[string]int
}

func (r conversionReport) log() {
	log.Printf("collapsed %d repeated rows", r.repeatedRows)
	if len(r.mergedMuscles) > 0 {
		log.Printf("%d slugs appeared under more than one muscle and were merged: %v", len(r.mergedMuscles), r.mergedMuscles)
	}
	for muscle, count := range r.unmappedMuscles {
		log.Printf("WARNING: no muscle mapping for %q (%d rows) — those rows have no primary muscle", muscle, count)
	}
	for gear, count := range r.unmappedGear {
		log.Printf("WARNING: no equipment mapping for %q (%d rows) — defaulted to \"other\"", gear, count)
	}
	if r.withoutPrimaries > 0 {
		log.Printf("WARNING: %d exercises have no primary muscle at all", r.withoutPrimaries)
	}
	// A curated slug matching nothing is the quietest failure here: the entry
	// looks like curated data, reviews like curated data, and does nothing.
	for _, slug := range r.orphanedCurations {
		log.Printf("WARNING: secondaryMuscles has %q, which matches no exercise — fix the slug or drop the entry", slug)
	}
	for change, count := range r.equipmentCorrections {
		log.Printf("corrected equipment %s for %d rows (the validator's rules win over the source)", change, count)
	}
	log.Printf("%d exercises carry curated secondary muscles (of %d curated entries)", r.withSecondaries, len(secondaryMuscles))
}

// convert maps FitMe's rows onto North's vocabulary, merging rows that share a
// slug.
//
// Merging rather than dropping, because the source was fetched one muscle at a
// time and the same movement appears under each muscle it trains. Most repeats
// are byte-identical and merging them is a no-op, but a few — the power snatch
// is filed under both hamstrings and quadriceps — carry a different muscle in
// each copy. Keeping the first and discarding the rest would throw away a true
// attribution and leave the model highlighting half the lift.
func convert(rows []row) ([]exercise, conversionReport) {
	report := conversionReport{
		unmappedMuscles:      map[string]int{},
		unmappedGear:         map[string]int{},
		equipmentCorrections: map[string]int{},
	}

	bySlug := map[string]*exercise{}
	var order []string

	for _, r := range rows {
		name := cleanName(r.Name)
		if name == "" {
			continue
		}
		slug := slugify(name)

		var primary []string
		if key, ok := muscleMap[r.Muscle]; ok {
			primary = []string{key}
		} else {
			report.unmappedMuscles[r.Muscle]++
		}

		gear, ok := equipmentMap[strings.ToLower(r.Equipment)]
		if !ok {
			report.unmappedGear[r.Equipment]++
			gear = "other"
		}

		// The validator's own reading of the name wins over the source's
		// claim. The source files a pull-up as "body_only"; the validator
		// reads "pull-up" and requires a bar. Seeding the source's answer
		// would let the catalog recommend bar work to someone with no bar,
		// and the validator would then reject the plan it had just suggested.
		if inferred := plan.InferEquipment(name, r.Equipment); inferred != "" && inferred != gear {
			report.equipmentCorrections[gear+" -> "+inferred]++
			gear = inferred
		}

		existing, seen := bySlug[slug]
		if !seen {
			bySlug[slug] = &exercise{
				Slug:         slug,
				Name:         name,
				Category:     r.Type,
				Equipment:    gear,
				Difficulty:   r.Difficulty,
				Instructions: strings.TrimSpace(r.Instructions),
				Video:        strings.TrimSpace(r.Video),
				Primary:      primary,
				Secondary:    secondaryMuscles[slug],
			}
			order = append(order, slug)
			continue
		}

		report.repeatedRows++

		before := len(existing.Primary)
		existing.Primary = appendMissing(existing.Primary, primary)
		if len(existing.Primary) != before {
			report.mergedMuscles = append(report.mergedMuscles, slug)
		}

		// The fullest instructions win. Where two copies disagree, the longer
		// one is the one that describes the whole movement — the shorter is
		// the summary the source stored under the secondary muscle.
		if instructions := strings.TrimSpace(r.Instructions); len(instructions) > len(existing.Instructions) {
			existing.Instructions = instructions
			if video := strings.TrimSpace(r.Video); video != "" {
				existing.Video = video
			}
		}
	}

	for slug := range secondaryMuscles {
		if _, matched := bySlug[slug]; !matched {
			report.orphanedCurations = append(report.orphanedCurations, slug)
		}
	}
	sort.Strings(report.orphanedCurations)

	out := make([]exercise, 0, len(order))
	for _, slug := range order {
		e := bySlug[slug]
		if len(e.Primary) == 0 {
			report.withoutPrimaries++
		}
		if len(e.Secondary) > 0 {
			report.withSecondaries++
		}
		// A merge can leave a muscle in both lists; primary wins.
		e.Secondary = removeAll(e.Secondary, e.Primary)
		out = append(out, *e)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, report
}

func appendMissing(into, extra []string) []string {
	for _, candidate := range extra {
		if !contains(into, candidate) {
			into = append(into, candidate)
		}
	}
	return into
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

// cleanName strips the stray ')' that prefixes every name in FitMe's seed — an
// artifact of however the file was generated, present on all 304 rows.
func cleanName(name string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), ")"))
}

func slugify(name string) string {
	var b strings.Builder
	lastDash := true // leading dashes suppressed

	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}

	return strings.Trim(b.String(), "-")
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

func render(exercises []exercise) string {
	var b strings.Builder

	fmt.Fprintf(&b, `-- +goose Up
-- +goose StatementBegin

-- Generated by scripts/fitme-exercises on %s. Do not hand-edit: regenerate.
--
-- Source: the FitMe project's 010_insert_exercises.sql, itself derived from a
-- public exercise dataset. The muscle and equipment vocabularies were
-- translated into North's own (see the maps in that script), the stray ')'
-- prefix on every source name was stripped, and duplicate movements — the
-- source lists some under two muscles — were dropped.
--
-- secondary_muscles is curated by hand for the compound lifts only. The source
-- carries one muscle per exercise, so anything else would be a guess printed
-- onto an anatomical model.

INSERT INTO exercises (slug, name, category, equipment, difficulty, instructions, video_url, primary_muscles, secondary_muscles) VALUES
`, time.Now().UTC().Format("2006-01-02"))

	for i, e := range exercises {
		terminator := ","
		if i == len(exercises)-1 {
			terminator = ";"
		}
		fmt.Fprintf(&b, "    (%s, %s, %s, %s, %s, %s, %s, %s, %s)%s\n",
			quote(e.Slug), quote(e.Name), quote(e.Category), quote(e.Equipment),
			quote(e.Difficulty), quote(e.Instructions), quote(e.Video),
			textArray(e.Primary), textArray(e.Secondary), terminator)
	}

	b.WriteString(`
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM exercises;
-- +goose StatementEnd
`)

	return b.String()
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
