// Command exercise-cues fills in written instructions for catalogue rows that
// have artwork and no text.
//
// 269 of 455 exercises arrived from the illustration import with a picture and
// nothing to read, so asking the coach how to perform one got an honest "not
// recorded in the catalogue" and a drawing. This closes part of that.
//
// Run it from the repository root:
//
//	curl -sL -o /tmp/free-exercise-db.json \
//	  https://raw.githubusercontent.com/yuhonas/free-exercise-db/main/dist/exercises.json
//	psql -tAc "COPY (SELECT slug||'|'||name||'|'||equipment FROM exercises \
//	  WHERE illustration_slug <> '' AND length(instructions)=0) TO STDOUT" > /tmp/gap.txt
//	go run ./scripts/exercise-cues \
//	  -source  /tmp/free-exercise-db.json \
//	  -targets /tmp/gap.txt \
//	  -out     migrations/<timestamp>_exercise_cues.sql
//
// # Why this source and no other
//
// Three datasets were measured against the gap before one was chosen:
//
//	everkinetic/data           3 of 269   CC BY-SA 4.0
//	exercemus/exercises        1 of 269   MIT
//	yuhonas/free-exercise-db  38 of 269   Unlicense
//
// The union is 45 once fuzzier token matching is allowed, but this command
// takes exact names only, and on that basis free-exercise-db supplies 38 of
// the 42 the other two could add. The extra four cost far more than they are
// worth. Everkinetic's text is CC BY-SA,
// and ShareAlike would follow it into the instructions column and out to every
// surface that renders one — a licence obligation on the whole catalogue for
// three rows. Exercemus is MIT, which needs its notice carried, for one row.
//
// free-exercise-db is Unlicense: public domain, no attribution obligation, no
// downstream condition. That is the only reason it is the sole source here.
// The artwork's CC BY-SA still applies to the artwork; nothing about this
// changes that, and web/assets/exercises/NOTICE remains correct.
//
// # What it cannot do
//
// It leaves 231 rows still without cues. The two catalogues name the same
// movements differently — "Machine Lateral Raise" against "Lateral Raise -
// With Bands" — and the residue is real disagreement rather than a matching
// bug. Widening the match is how you introduce a squat's instructions onto a
// deadlift, so this only takes an exact name match, and everything else stays
// honestly empty.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// upstream is one record in free-exercise-db's dist/exercises.json.
type upstream struct {
	Name         string   `json:"name"`
	Instructions []string `json:"instructions"`
}

func main() {
	source := flag.String("source", "", "path to free-exercise-db dist/exercises.json")
	targets := flag.String("targets", "", "file of catalogue names still lacking cues, one per line")
	out := flag.String("out", "", "path to write the generated migration to")
	flag.Parse()

	if *source == "" || *out == "" || *targets == "" {
		log.Fatal("usage: exercise-cues -source <exercises.json> -targets <names.txt> -out <migration.sql>")
	}

	// The gap, read from the live catalogue. Without it the migration would
	// carry an UPDATE for all 871 upstream records — harmless, since only empty
	// rows match, but 3,500 lines nobody can review to find the 41 that matter.
	wanted, err := readTargets(*targets)
	if err != nil {
		log.Fatalf("reading the target names: %v", err)
	}

	raw, err := os.ReadFile(*source)
	if err != nil {
		log.Fatalf("reading the upstream dataset: %v", err)
	}

	var entries []upstream
	if err := json.Unmarshal(raw, &entries); err != nil {
		log.Fatalf("parsing the upstream dataset: %v", err)
	}

	// Keyed on the normalised name, which is the only join available: the two
	// catalogues share no ids and no slug vocabulary.
	byName := make(map[string]string, len(entries))
	for _, e := range entries {
		text := strings.TrimSpace(strings.Join(e.Instructions, " "))
		if text == "" {
			continue
		}
		key := normalise(e.Name)
		if key == "" || !wanted[key] {
			continue
		}
		// First wins. Duplicate names upstream are variants of one movement and
		// picking between them by position would be arbitrary either way.
		if _, seen := byName[key]; !seen {
			byName[key] = text
		}
	}

	if err := write(*out, byName); err != nil {
		log.Fatalf("writing the migration: %v", err)
	}

	fmt.Printf("%d of %d rows lacking cues matched upstream\n", len(byName), len(wanted))
}

// readTargets loads the names of catalogue rows that still have no cues.
func readTargets(path string) (map[string]bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	wanted := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		// slug|name|equipment, as exported from the catalogue.
		parts := strings.Split(strings.TrimSpace(line), "|")
		if len(parts) < 2 {
			continue
		}
		if key := normalise(parts[1]); key != "" {
			wanted[key] = true
		}
	}
	return wanted, nil
}

var notAlnum = regexp.MustCompile(`[^a-z0-9]`)

// normalise reduces a name to the form both catalogues can be compared in.
func normalise(s string) string { return notAlnum.ReplaceAllString(strings.ToLower(s), "") }

// write emits the migration.
//
// The match happens in SQL rather than here, against the live rows, so the
// script does not need a database and the migration stays honest about which
// rows it touched: only those still empty at the moment it runs.
func write(path string, byName map[string]string) error {
	keys := make([]string, 0, len(byName))
	for k := range byName {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	fmt.Fprintf(&b, `-- +goose Up
-- Written cues for catalogue rows that had artwork and no text.
--
-- Generated by scripts/exercise-cues on %s from
-- github.com/yuhonas/free-exercise-db (Unlicense — public domain, no
-- attribution obligation and no condition that follows the text downstream).
-- That licence is why this source and not the two better-known alternatives:
-- see the command's doc comment for the measurements.
--
-- Matched on the exact normalised name, which is the only join the two
-- catalogues share. Deliberately narrow: a looser match is how a squat's
-- instructions end up on a deadlift, and a row with no cues is a much smaller
-- problem than a row with the wrong ones.
--
-- Only rows that are still empty are touched, so this cannot overwrite text
-- somebody wrote by hand, and re-running it is a no-op.

`, time.Now().UTC().Format("2006-01-02"))

	for _, key := range keys {
		fmt.Fprintf(&b, "UPDATE exercises SET instructions = %s, updated_at = now()\n"+
			" WHERE regexp_replace(lower(name), '[^a-z0-9]', '', 'g') = %s\n"+
			"   AND instructions = '';\n\n",
			quote(byName[key]), quote(key))
	}

	b.WriteString("-- +goose Down\n")
	b.WriteString("-- Not reversible by row: the column had no other content to restore, and\n")
	b.WriteString("-- emptying every match would also discard anything written since. Rows that\n")
	b.WriteString("-- were filled here stay filled.\n")
	b.WriteString("SELECT 1;\n")

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// quote renders a SQL string literal, doubling any embedded quote.
func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
