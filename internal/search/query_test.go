package search_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/search"
	"github.com/NorthAIProject/north-client/internal/shared/limits"
)

func TestNormaliseRejectsEmpty(t *testing.T) {
	for _, term := range []string{"", "   ", "\n\t "} {
		if _, err := search.Normalise(term); !errors.Is(err, search.ErrEmptyTerm) {
			t.Errorf("Normalise(%q) error = %v, want ErrEmptyTerm", term, err)
		}
	}
}

func TestNormaliseCollapsesWhitespace(t *testing.T) {
	got, err := search.Normalise("  knee   pain\n\tafter  squats ")
	if err != nil {
		t.Fatal(err)
	}
	if want := "knee pain after squats"; got != want {
		t.Errorf("Normalise = %q, want %q", got, want)
	}
}

func TestNormaliseBoundsLength(t *testing.T) {
	ok := strings.Repeat("a", limits.MaxSearchTermLength)
	if _, err := search.Normalise(ok); err != nil {
		t.Errorf("term at the limit was rejected: %v", err)
	}

	if _, err := search.Normalise(ok + "a"); err == nil {
		t.Error("term over the limit was accepted")
	}
}

// TestNormaliseLeavesOperatorsAlone is the counterpart to the query-safety test
// in internal/memories: Normalise must not try to sanitise operator characters
// itself. websearch_to_tsquery is what makes them safe, and a second layer of
// escaping here would quietly change what the user asked for.
func TestNormaliseLeavesOperatorsAlone(t *testing.T) {
	const hostile = `AND OR NOT NEAR( ") ' -- ; DROP`

	got, err := search.Normalise(hostile)
	if err != nil {
		t.Fatal(err)
	}
	if got != hostile {
		t.Errorf("Normalise altered the term: %q -> %q", hostile, got)
	}
}

// TestNormaliseCountsRunesNotBytes guards the length bound against multi-byte
// text: a 1000-character message in a language that uses three bytes a
// character is a normal message, not an attack.
func TestNormaliseCountsRunesNotBytes(t *testing.T) {
	term := strings.Repeat("日", limits.MaxSearchTermLength)
	if _, err := search.Normalise(term); err != nil {
		t.Errorf("rejected %d runes (%d bytes): %v", limits.MaxSearchTermLength, len(term), err)
	}
}
