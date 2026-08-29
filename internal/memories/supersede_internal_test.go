package memories

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/memories/extract"
)

// The index the model returns is 1-based and 0 means "replaces nothing". Off by
// one here retires the wrong fact, which is the one outcome this whole feature
// is built to avoid, so the mapping gets its own test.
func TestResolveSupersessionsMapsTheIndexToTheRightFact(t *testing.T) {
	t.Parallel()

	first, second, third := uuid.New(), uuid.New(), uuid.New()
	believed := []CurrentFact{
		{ID: first, Content: "trains five days a week"},
		{ID: second, Content: "owns dumbbells"},
		{ID: third, Content: "has a shoulder injury"},
	}

	got := resolveSupersessions([]extract.Candidate{
		{Content: "trains three days a week", Supersedes: 1},
		{Content: "the shoulder is fine now", Supersedes: 3},
		{Content: "bought a squat rack", Supersedes: 0},
	}, believed)

	if len(got) != 3 {
		t.Fatalf("got %d proposals, want 3", len(got))
	}

	if got[0].SupersedesID == nil || *got[0].SupersedesID != first {
		t.Errorf("index 1 resolved to %v, want the first fact", got[0].SupersedesID)
	}
	if got[1].SupersedesID == nil || *got[1].SupersedesID != third {
		t.Errorf("index 3 resolved to %v, want the third fact", got[1].SupersedesID)
	}
	if got[2].SupersedesID != nil {
		t.Errorf("index 0 resolved to %v, want nothing", got[2].SupersedesID)
	}
}

// An index past the end of the list must resolve to nothing rather than to
// whatever happens to be last. Sanitise already zeroes these, so this is the
// second line of defence — and the one that matters if the list ever gets
// re-ordered between the prompt and the resolution.
func TestResolveSupersessionsRefusesAnIndexOutOfRange(t *testing.T) {
	t.Parallel()

	believed := []CurrentFact{{ID: uuid.New(), Content: "only fact"}}

	for _, index := range []int{-1, 2, 99} {
		got := resolveSupersessions([]extract.Candidate{
			{Content: "some new fact", Supersedes: index},
		}, believed)

		if got[0].SupersedesID != nil {
			t.Errorf("index %d resolved to %v, want nothing", index, got[0].SupersedesID)
		}
	}
}

func TestResolveSupersessionsWithNothingBelieved(t *testing.T) {
	t.Parallel()

	got := resolveSupersessions([]extract.Candidate{
		{Content: "a first fact", Supersedes: 1},
	}, nil)

	if got[0].SupersedesID != nil {
		t.Error("resolved a supersession against an empty list")
	}
}

// The numbering in the prompt has to start at 1, because 0 is the schema's
// "replaces nothing" and a zero-based list would collide with it on the most
// likely value a model emits when it has no opinion.
func TestFormatBelievedNumbersFromOne(t *testing.T) {
	t.Parallel()

	got := formatBelieved([]CurrentFact{
		{Category: "habit", Content: "trains five days a week"},
		{Category: "injury", Content: "has a shoulder injury", Pinned: true},
	})

	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2:\n%s", len(lines), got)
	}
	if !strings.HasPrefix(lines[0], "1. ") {
		t.Errorf("first line is %q, want it to start at 1", lines[0])
	}
	if !strings.HasPrefix(lines[1], "2. ") {
		t.Errorf("second line is %q", lines[1])
	}
	if !strings.Contains(lines[0], "habit") || !strings.Contains(lines[0], "trains five days") {
		t.Errorf("first line lost its category or content: %q", lines[0])
	}

	// Pinned is shown so the model can still say a pinned fact is out of date.
	// It is the store that refuses to act on that without a human, not the
	// prompt that hides the fact.
	if !strings.Contains(lines[1], "pinned") {
		t.Errorf("pinned fact is not marked: %q", lines[1])
	}
}

func TestFormatBelievedWithNothingRecorded(t *testing.T) {
	t.Parallel()

	got := formatBelieved(nil)
	if got == "" {
		t.Fatal("an empty list rendered as an empty string, which would leave the prompt section blank")
	}
	if strings.Contains(got, "1.") {
		t.Errorf("rendered a numbered entry for an empty list: %q", got)
	}
}
