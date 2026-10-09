package agent

import (
	"context"
	"strings"
	"testing"
	"time"
)

func entriesFixture() []loggedEntry {
	base := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	noop := func(context.Context) error { return nil }
	return []loggedEntry{
		{label: "Espresso (63 mg caffeine)", at: base, undo: noop},
		{label: "Filter coffee (95 mg caffeine)", at: base.Add(3 * time.Hour), undo: noop},
		{label: "Espresso (63 mg caffeine)", at: base.Add(5 * time.Hour), undo: noop},
	}
}

// "Undo that" means the thing just logged.
func TestPickEntryTakesTheNewestWhenNothingNarrowsIt(t *testing.T) {
	t.Parallel()

	got, err := pickEntry(entriesFixture(), "caffeine", "", time.UTC)
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if got.at.Hour() != 13 {
		t.Errorf("picked the entry at %s, want the newest", got.at.Format("15:04"))
	}
}

func TestPickEntryMatchesOnPartOfTheDescription(t *testing.T) {
	t.Parallel()

	got, err := pickEntry(entriesFixture(), "caffeine", "filter", time.UTC)
	if err != nil {
		t.Fatalf("pick: %v", err)
	}
	if !strings.HasPrefix(got.label, "Filter coffee") {
		t.Errorf("picked %q", got.label)
	}
}

// Two espressos: removing either silently would be a guess, and the error
// has to carry the times so the model can ask which.
func TestPickEntryRefusesToGuessBetweenMatches(t *testing.T) {
	t.Parallel()

	_, err := pickEntry(entriesFixture(), "caffeine", "espresso", time.UTC)
	if err == nil {
		t.Fatal("an ambiguous match was accepted")
	}
	for _, want := range []string{"08:00", "13:00"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %s: %v", want, err)
		}
	}
}

func TestPickEntryListsTodayWhenNothingMatches(t *testing.T) {
	t.Parallel()

	_, err := pickEntry(entriesFixture(), "caffeine", "matcha", time.UTC)
	if err == nil || !strings.Contains(err.Error(), "Filter coffee") {
		t.Errorf("error = %v, want it to list today's entries", err)
	}
	if _, err := pickEntry(nil, "water", "", time.UTC); err == nil {
		t.Error("an empty day was accepted")
	}
}

// Undoing deletes a log, so it must always go through an approval card.
func TestUndoLogIsAWriteAndOffersOnlyWiredKinds(t *testing.T) {
	t.Parallel()

	c := undoLog(undoSources(Services{}), nil)
	if c.ReadOnly {
		t.Error("undo_log is marked ReadOnly, so it would delete with no approval card")
	}
	if got := c.Tool.Parameters.Properties["kind"].Enum; len(got) != 0 {
		t.Errorf("with nothing wired the kinds are %v, want none", got)
	}
}
