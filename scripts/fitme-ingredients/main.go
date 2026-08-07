// Command fitme-ingredients converts the FitMe project's food seed into a
// goose migration for North's ingredients table.
//
// # The column order
//
// This script exists because FitMe's seed declares one column order and stores
// another. Its INSERT names:
//
//	name, calories, serving_size, protein, fat_total, fat_saturated,
//	carbohydrates_total, fiber, sugar, sodium, potassium, cholesterol
//
// but the values are in the order the upstream nutrition API returns them:
//
//	name, calories, serving_size, fat_total, fat_saturated, protein,
//	sodium, potassium, cholesterol, carbohydrates_total, fiber, sugar
//
// Read the declared way, an egg has 9.7g of protein and 139g of fibre. Read
// the real way it has 12.5g of protein, 9.7g of fat, and 139mg of sodium,
// which is an egg. The mapping was confirmed against butter (79.6g fat, 51.1g
// saturated, 215mg cholesterol), olive oil, chicken breast, almonds, and
// broccoli before being relied on.
//
// A straight copy of the source file would therefore have stored fibre values
// in the sodium column, silently, for every one of 1,528 rows.
//
// Run it from the repository root:
//
//	go run ./scripts/fitme-ingredients \
//	  -in  ../../Projects/FitME/fitme-grpc/internal/migrations/011_insert_food.sql \
//	  -out migrations/00026_seed_ingredients.sql
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

func main() {
	in := flag.String("in", "", "path to FitMe's 011_insert_food.sql")
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

	ingredients, report := convert(rows)
	report.log()

	if err := os.WriteFile(*out, []byte(render(ingredients)), 0o644); err != nil {
		log.Fatalf("writing the migration: %v", err)
	}
	log.Printf("wrote %d ingredients to %s", len(ingredients), *out)
}

// ---------------------------------------------------------------------------
// Parsing
// ---------------------------------------------------------------------------

// row is one source tuple, named for what the values actually are rather than
// for what the source's column list claims. See the package comment.
type row struct {
	Name string

	Calories    float64
	ServingSize float64

	FatTotal     float64
	FatSaturated float64
	Protein      float64

	SodiumMg      float64
	PotassiumMg   float64
	CholesterolMg float64

	Carbs float64
	Fiber float64
	Sugar float64
}

// fieldsPerRow is one name plus eleven numbers. Asserted per tuple: a row with
// a different count means the source's shape is not what this script was
// written against, and guessing which field is missing would corrupt the data
// in exactly the way this script exists to prevent.
const fieldsPerRow = 12

func parse(source string) ([]row, error) {
	chunks := strings.Split(source, "(gen_random_uuid(),")
	if len(chunks) < 2 {
		return nil, fmt.Errorf("found no `(gen_random_uuid(),` tuples — is this the right file?")
	}

	var rows []row
	for i, chunk := range chunks[1:] {
		name, numbers, err := scanTuple(chunk)
		if err != nil {
			return nil, fmt.Errorf("tuple %d: %w", i+1, err)
		}
		if 1+len(numbers) != fieldsPerRow {
			return nil, fmt.Errorf("tuple %d (%q) has %d numeric fields, want %d", i+1, name, len(numbers), fieldsPerRow-1)
		}

		rows = append(rows, row{
			Name:          name,
			Calories:      numbers[0],
			ServingSize:   numbers[1],
			FatTotal:      numbers[2],
			FatSaturated:  numbers[3],
			Protein:       numbers[4],
			SodiumMg:      numbers[5],
			PotassiumMg:   numbers[6],
			CholesterolMg: numbers[7],
			Carbs:         numbers[8],
			Fiber:         numbers[9],
			Sugar:         numbers[10],
		})
	}
	return rows, nil
}

// scanTuple reads the leading quoted name and then every number up to the
// tuple's closing paren.
func scanTuple(chunk string) (string, []float64, error) {
	var name string
	var numbers []float64
	seenName := false

	for i := 0; i < len(chunk); i++ {
		switch c := chunk[i]; {
		case c == ')':
			return name, numbers, nil

		case c == '\'':
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
			if seenName {
				return "", nil, fmt.Errorf("a second string field %q, but only the name should be a string", b.String())
			}
			name, seenName = b.String(), true
			i = j

		case c >= '0' && c <= '9', c == '-' && i+1 < len(chunk) && chunk[i+1] >= '0' && chunk[i+1] <= '9':
			j := i
			for j < len(chunk) && (chunk[j] == '.' || chunk[j] == '-' || (chunk[j] >= '0' && chunk[j] <= '9')) {
				j++
			}
			value, err := strconv.ParseFloat(chunk[i:j], 64)
			if err != nil {
				return "", nil, fmt.Errorf("parsing %q: %w", chunk[i:j], err)
			}
			numbers = append(numbers, value)
			i = j - 1
		}
	}

	return name, numbers, nil
}

// ---------------------------------------------------------------------------
// Conversion
// ---------------------------------------------------------------------------

type ingredient struct {
	Name string

	Calories     float64
	Protein      float64
	Fat          float64
	SaturatedFat float64
	Carbs        float64
	Fiber        float64
	Sugar        float64

	SodiumMg      float64
	PotassiumMg   float64
	CholesterolMg float64
}

type conversionReport struct {
	duplicates int
	rejected   []string
}

func (r conversionReport) log() {
	log.Printf("dropped %d duplicate names", r.duplicates)
	// Every rejection is named. A silent drop count is a number nobody can
	// check; a list is something a reviewer can disagree with.
	for _, reason := range r.rejected {
		log.Printf("REJECTED %s", reason)
	}
	log.Printf("%d rows rejected by the sanity gate", len(r.rejected))
}

// macroCeiling is the grams of protein + fat + carbohydrate allowed per 100g
// before a row is treated as corrupt.
//
// 105 rather than 100: the source rounds, and pure fats land slightly over
// (its olive oil is 101.2g of fat per 100g). The gate is here to catch a
// column that has shifted by one — which produces sodium in milligrams landing
// in a gram column, and numbers in the hundreds — not to audit rounding.
const macroCeiling = 105

func convert(rows []row) ([]ingredient, conversionReport) {
	var report conversionReport

	seen := map[string]bool{}
	var out []ingredient

	for _, r := range rows {
		name := cleanName(r.Name)
		if name == "" {
			continue
		}

		key := strings.ToLower(name)
		if seen[key] {
			report.duplicates++
			continue
		}

		// The source stores everything per 100g and says so in its own
		// serving_size column. Asserted rather than assumed: if a row were
		// per-serving, copying its numbers into per-100g columns would
		// misstate it by whatever the serving happened to be.
		if r.ServingSize != 100 {
			report.rejected = append(report.rejected,
				fmt.Sprintf("%q: serving size is %g, not 100g", name, r.ServingSize))
			continue
		}

		// Zero calories is not a defect: salt, sparkling water, black coffee
		// and stevia are all real things to log, and salt's whole point is
		// its sodium. Only a row with nothing in it at all is dropped.
		if r.Calories < 0 {
			report.rejected = append(report.rejected,
				fmt.Sprintf("%q: %g calories", name, r.Calories))
			continue
		}

		if isEmpty(r) {
			report.rejected = append(report.rejected,
				fmt.Sprintf("%q: every nutrient is zero", name))
			continue
		}

		if total := r.Protein + r.FatTotal + r.Carbs; total > macroCeiling {
			report.rejected = append(report.rejected,
				fmt.Sprintf("%q: %.1fg of protein+fat+carbs per 100g", name, total))
			continue
		}

		if r.Protein < 0 || r.FatTotal < 0 || r.Carbs < 0 {
			report.rejected = append(report.rejected,
				fmt.Sprintf("%q: a negative macro", name))
			continue
		}

		seen[key] = true
		out = append(out, ingredient{
			Name:          name,
			Calories:      r.Calories,
			Protein:       r.Protein,
			Fat:           r.FatTotal,
			SaturatedFat:  r.FatSaturated,
			Carbs:         r.Carbs,
			Fiber:         r.Fiber,
			Sugar:         r.Sugar,
			SodiumMg:      r.SodiumMg,
			PotassiumMg:   r.PotassiumMg,
			CholesterolMg: r.CholesterolMg,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, report
}

// isEmpty reports whether a row carries no nutritional information at all,
// which is the only shape that is certainly a defect rather than a food.
func isEmpty(r row) bool {
	for _, value := range []float64{
		r.Calories, r.Protein, r.FatTotal, r.FatSaturated, r.Carbs,
		r.Fiber, r.Sugar, r.SodiumMg, r.PotassiumMg, r.CholesterolMg,
	} {
		if value != 0 {
			return false
		}
	}
	return true
}

// cleanName strips the stray ')' that prefixes names in the source and
// capitalises the first letter, since the source stores everything lowercase
// and these are shown to people.
func cleanName(name string) string {
	name = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(name), ")"))
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return ""
	}

	runes := []rune(name)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// ---------------------------------------------------------------------------
// Rendering
// ---------------------------------------------------------------------------

func render(ingredients []ingredient) string {
	var b strings.Builder

	fmt.Fprintf(&b, `-- +goose Up
-- +goose StatementBegin

-- Generated by scripts/fitme-ingredients on %s. Do not hand-edit: regenerate.
--
-- Source: the FitMe project's 011_insert_food.sql. That file's INSERT declares
-- its columns in one order and stores them in another, so the values here are
-- re-mapped — see the script's package comment for the two orders and how the
-- correct one was confirmed. Reading the source as declared gives an egg 139g
-- of fibre.
--
-- user_id is NULL: these are the shared ingredients anyone can log against,
-- per the comment on the table in 00014.

INSERT INTO ingredients (
    user_id, name, category, serving_size_grams,
    calories_per_100g, protein_g_per_100g, fat_g_per_100g, saturated_fat_g_per_100g,
    carbs_g_per_100g, fiber_g_per_100g, sugar_g_per_100g,
    sodium_mg_per_100g, potassium_mg_per_100g, cholesterol_mg_per_100g
) VALUES
`, time.Now().UTC().Format("2006-01-02"))

	for i, in := range ingredients {
		terminator := ","
		if i == len(ingredients)-1 {
			terminator = ";"
		}
		fmt.Fprintf(&b, "    (NULL, %s, 'other', 100, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s)%s\n",
			quote(in.Name),
			number(in.Calories), number(in.Protein), number(in.Fat), number(in.SaturatedFat),
			number(in.Carbs), number(in.Fiber), number(in.Sugar),
			number(in.SodiumMg), number(in.PotassiumMg), number(in.CholesterolMg),
			terminator)
	}

	b.WriteString(`
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Only the shared rows. A user's own ingredients are not this migration's to
-- delete, and rolling back a seed must not take someone's data with it.
DELETE FROM ingredients WHERE user_id IS NULL;
-- +goose StatementEnd
`)

	return b.String()
}

func quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func number(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
