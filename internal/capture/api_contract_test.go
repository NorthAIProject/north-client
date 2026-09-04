package capture_test

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/capture"
)

var update = flag.Bool("update", false, "rewrite the capture API contract golden files")

// The JSON a client sees is a contract, and the failure that matters is a field
// renamed or dropped in a refactor. Per-field assertions miss exactly that,
// because nobody writes an assertion for a field they deleted — so the whole
// shape is pinned instead, built from fixed values with no database and no
// model involved.
func TestParseResponseShape(t *testing.T) {
	t.Parallel()

	assertGolden(t, "parse.golden.json", capture.ParseResponse{
		Items: []capture.Item{
			{
				Kind:   capture.KindWater,
				Source: "2L water",
				Water:  &capture.Water{AmountML: 2000},
			},
			{
				Kind:      capture.KindFood,
				Source:    "a bit of chicken",
				Uncertain: true,
				Food: &capture.Food{
					Query:        "chicken breast",
					Grams:        150,
					IngredientID: uuid.MustParse("11111111-1111-1111-1111-111111111111"),
					MatchedName:  "Chicken breast, raw",
				},
			},
			{
				Kind:    capture.KindHabit,
				Source:  "meditated",
				Habit:   &capture.Habit{Name: "Meditate"},
				Problem: "You are not keeping a habit called \"Meditate\".",
			},
		},
		Unparsed: []string{"went for a long walk by the river"},
	})
}

func TestCommitResponseShape(t *testing.T) {
	t.Parallel()

	assertGolden(t, "commit.golden.json", capture.CommitResponse{
		Written: 1,
		Failed:  1,
		Outcomes: []capture.Outcome{
			{
				Item:    capture.Item{Kind: capture.KindWater, Source: "2L water", Water: &capture.Water{AmountML: 2000}},
				Summary: "Logged 2000 ml of water.",
			},
			{
				Item:  capture.Item{Kind: capture.KindWeight, Source: "78kg", Weight: &capture.Weight{KG: 78}},
				Error: "No measurement to update; record height, date of birth and sex once first.",
			},
		},
		Skipped: []capture.Item{
			{
				Kind:    capture.KindHabit,
				Source:  "meditated",
				Habit:   &capture.Habit{Name: "Meditate"},
				Problem: "You are not keeping a habit called \"Meditate\".",
			},
		},
	})
}

func assertGolden(t *testing.T, name string, value any) {
	t.Helper()

	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	encoded = append(encoded, '\n')

	path := filepath.Join("testdata", name)
	if *update {
		if writeErr := os.WriteFile(path, encoded, 0o644); writeErr != nil {
			t.Fatalf("write %s: %v", path, writeErr)
		}
		t.Logf("wrote %s", path)
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s (run with -update to create it): %v", path, err)
	}
	if string(want) != string(encoded) {
		t.Errorf("the capture API shape changed. Review the diff — it is what every client sees.\n\nwant:\n%s\n\ngot:\n%s", want, encoded)
	}
}
