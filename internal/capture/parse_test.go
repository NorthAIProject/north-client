package capture

import (
	"fmt"
	"strings"
	"testing"
	"time"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func at(t *testing.T) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	return time.Date(2026, 9, 4, 9, 0, 0, 0, loc)
}

func TestConvertBuildsEachKind(t *testing.T) {
	t.Parallel()

	now := at(t)

	cases := []struct {
		name string
		raw  modelItem
		want func(Item) error
	}{
		{
			name: "water",
			raw:  modelItem{Kind: "water", Source: "2L water", AmountML: 2000},
			want: func(i Item) error {
				if i.Water == nil || i.Water.AmountML != 2000 {
					return errf("water = %+v", i.Water)
				}
				return nil
			},
		},
		{
			name: "sleep",
			raw:  modelItem{Kind: "sleep", Source: "slept 6h", Minutes: 360, Quality: 4},
			want: func(i Item) error {
				if i.Sleep == nil || i.Sleep.Minutes != 360 || i.Sleep.Quality != 4 {
					return errf("sleep = %+v", i.Sleep)
				}
				return nil
			},
		},
		{
			name: "habit",
			raw:  modelItem{Kind: "habit", Source: "read 20 pages", Habit: "Read 20 pages"},
			want: func(i Item) error {
				if i.Habit == nil || i.Habit.Name != "Read 20 pages" {
					return errf("habit = %+v", i.Habit)
				}
				return nil
			},
		},
		{
			name: "check in",
			raw:  modelItem{Kind: "check_in", Source: "mood 4 energy 3", Mood: 4, Energy: 3},
			want: func(i Item) error {
				if i.CheckIn == nil || i.CheckIn.Mood != 4 || i.CheckIn.Energy != 3 {
					return errf("check-in = %+v", i.CheckIn)
				}
				return nil
			},
		},
		{
			name: "food",
			raw:  modelItem{Kind: "food", Source: "150g chicken", Food: "chicken breast", Grams: 150},
			want: func(i Item) error {
				if i.Food == nil || i.Food.Grams != 150 {
					return errf("food = %+v", i.Food)
				}
				return nil
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := convert(tc.raw, now)
			if err != nil {
				t.Fatalf("convert: %v", err)
			}
			if err := tc.want(got); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Reading "172 lb" as kilograms produces a number nobody would question and
// everybody would be wrong about, so the unit has to be honoured.
func TestConvertNormalisesPounds(t *testing.T) {
	t.Parallel()

	got, err := convert(modelItem{Kind: "weight", Source: "172 lb", Weight: 172, WeightUnit: "lb"}, at(t))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if got.Weight.KG != 78 {
		t.Fatalf("weight = %v kg, want 78", got.Weight.KG)
	}
}

func TestConvertRefusesAnUnknownWeightUnit(t *testing.T) {
	t.Parallel()

	_, err := convert(modelItem{Kind: "weight", Source: "12 stone", Weight: 12, WeightUnit: "stone"}, at(t))
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("want a validation error, got %v", err)
	}
}

func TestConvertRefusesOutOfRangeValues(t *testing.T) {
	t.Parallel()

	for name, raw := range map[string]modelItem{
		"water":  {Kind: "water", AmountML: 99000},
		"sleep":  {Kind: "sleep", Minutes: 5000},
		"weight": {Kind: "weight", Weight: 900, WeightUnit: "kg"},
		"mood":   {Kind: "check_in", Mood: 9, Energy: 3},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := convert(raw, at(t)); !apperr.Is(err, apperr.ErrValidation) {
				t.Fatalf("want a validation error, got %v", err)
			}
		})
	}
}

func TestConvertRefusesAFutureNight(t *testing.T) {
	t.Parallel()

	_, err := convert(modelItem{Kind: "sleep", Minutes: 360, Date: "2027-01-01"}, at(t))
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("want a validation error, got %v", err)
	}
}

// The coverage check is the point of the whole design: a model that forgets a
// clause must not be able to delete it.
func TestLeftoversCatchWhatTheModelForgot(t *testing.T) {
	t.Parallel()

	source := "2L water, slept 6h, went for a long walk by the river"
	draft := build(modelResult{
		Items: []modelItem{
			{Kind: "water", Source: "2L water", AmountML: 2000},
			{Kind: "sleep", Source: "slept 6h", Minutes: 360},
		},
		// The walk is reported nowhere: not an item, not unparsed.
		Unparsed: nil,
	}, source, at(t))

	if len(draft.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(draft.Items))
	}

	joined := strings.ToLower(strings.Join(draft.Unparsed, " | "))
	if !strings.Contains(joined, "walk") {
		t.Fatalf("the forgotten clause was dropped; unparsed = %v", draft.Unparsed)
	}
}

func TestLeftoversStaySilentWhenEverythingIsAccountedFor(t *testing.T) {
	t.Parallel()

	source := "2L water, slept 6h"
	draft := build(modelResult{
		Items: []modelItem{
			{Kind: "water", Source: "2L water", AmountML: 2000},
			{Kind: "sleep", Source: "slept 6h", Minutes: 360},
		},
	}, source, at(t))

	if len(draft.Unparsed) != 0 {
		t.Fatalf("unparsed = %v, want none", draft.Unparsed)
	}
}

// Removing "water" before "2L water" would leave "2L" behind and report it as
// missed, so spans are removed longest-first.
func TestLeftoversRemoveLongerSpansFirst(t *testing.T) {
	t.Parallel()

	draft := build(modelResult{
		Items: []modelItem{
			{Kind: "water", Source: "2L water", AmountML: 2000},
			{Kind: "habit", Source: "water the plants", Habit: "Water the plants"},
		},
	}, "2L water, water the plants", at(t))

	if len(draft.Unparsed) != 0 {
		t.Fatalf("unparsed = %v, want none", draft.Unparsed)
	}
}

// A mangled entry is shown back as text rather than failing the whole parse:
// the person said something, and swallowing it is the one outcome that makes a
// log untrustworthy.
func TestABadItemBecomesUnparsedRatherThanAnError(t *testing.T) {
	t.Parallel()

	draft := build(modelResult{
		Items: []modelItem{
			{Kind: "water", Source: "2L water", AmountML: 2000},
			{Kind: "weight", Source: "weighed a tonne", Weight: 5000, WeightUnit: "kg"},
		},
	}, "2L water, weighed a tonne", at(t))

	if len(draft.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(draft.Items))
	}
	if !strings.Contains(strings.Join(draft.Unparsed, " "), "tonne") {
		t.Fatalf("the rejected entry was dropped; unparsed = %v", draft.Unparsed)
	}
}

func TestLowConfidenceIsCarriedThrough(t *testing.T) {
	t.Parallel()

	got, err := convert(modelItem{Kind: "water", Source: "a glass", Confidence: "low", AmountML: 250}, at(t))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	if !got.Uncertain {
		t.Fatal("a low-confidence reading should be marked uncertain")
	}
}

func TestWritableRefusesUnresolvedNames(t *testing.T) {
	t.Parallel()

	if Writable(Item{Kind: KindHabit, Habit: &Habit{Name: "Read"}}) {
		t.Error("an unresolved habit must not be writable")
	}
	if Writable(Item{Kind: KindFood, Food: &Food{Query: "chicken", Grams: 100}}) {
		t.Error("an unresolved food must not be writable")
	}
	if Writable(Item{Kind: KindWater, Water: &Water{AmountML: 500}, Problem: "nope"}) {
		t.Error("an item with a problem must not be writable")
	}
	if !Writable(Item{Kind: KindWater, Water: &Water{AmountML: 500}}) {
		t.Error("a plain water item should be writable")
	}
}

func errf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}

// apperr.Wrap composes "message: sentinel", which is right for a log and wrong
// for a screen. This was found by reading the real receipt in a browser, where
// a fixable problem read "record height, date of birth and sex once first:
// validation failed".
func TestSentenceDropsTheSentinel(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		err  error
		want string
	}{
		"validation": {
			apperr.Wrap(apperr.ErrValidation, "record your height once first"),
			"Record your height once first.",
		},
		"not found": {
			apperr.Wrap(apperr.ErrNotFound, "no habit called %q", "Meditate"),
			`No habit called "Meditate".`,
		},
		"wrapped twice": {
			apperr.Wrap(apperr.Wrap(apperr.ErrValidation, "water 99000 is outside 1-5000"), "entry 2"),
			"Entry 2: water 99000 is outside 1-5000.",
		},
		"already punctuated": {
			apperr.Wrap(apperr.ErrValidation, "That is not valid."),
			"That is not valid.",
		},
		"nothing but a sentinel": {
			apperr.ErrValidation,
			"That did not work.",
		},
		"nil": {nil, ""},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := Sentence(tc.err); got != tc.want {
				t.Fatalf("Sentence() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The guarantee, rather than a list of examples: nothing a person reads may end
// in one of apperr's sentinel texts.
func TestNoUserFacingSentenceCarriesASentinel(t *testing.T) {
	t.Parallel()

	errs := []error{
		apperr.Wrap(apperr.ErrValidation, "water 99000 is outside 1-5000"),
		apperr.Wrap(apperr.ErrNotFound, "nothing matches"),
		apperr.Wrap(apperr.ErrConflict, "already logged"),
		apperr.Wrap(apperr.ErrUnavailable, "the model did not answer"),
		apperr.Wrap(apperr.ErrPaymentRequired, "out of budget"),
	}

	for _, err := range errs {
		got := Sentence(err)
		for _, suffix := range sentinelSuffixes {
			if strings.Contains(strings.ToLower(got), suffix) {
				t.Errorf("%q still carries the sentinel %q", got, suffix)
			}
		}
	}
}
