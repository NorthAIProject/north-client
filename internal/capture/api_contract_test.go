package capture_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/capture"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

// See package apitest for why whole shapes are pinned.
func TestParseResponseShape(t *testing.T) {
	t.Parallel()

	apitest.AssertGolden(t, "parse.golden.json", capture.ParseResponse{
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

	apitest.AssertGolden(t, "commit.golden.json", capture.CommitResponse{
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
