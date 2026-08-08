package search_test

import (
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/search"
)

type hit string

func (h hit) Key() string { return string(h) }

func keys(hits []hit) string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = string(h)
	}
	return strings.Join(out, ",")
}

// The whole reason for fusing rather than picking one retriever: a passage both
// methods liked should beat a passage only one of them loved.
func TestAgreementBeatsASingleStrongResult(t *testing.T) {
	text := []hit{"only-text", "agreed", "c"}
	vector := []hit{"only-vector", "agreed", "d"}

	got := search.Fuse(text, vector)

	if got[0] != "agreed" {
		t.Errorf("order = %s; the passage both retrievers found should rank first", keys(got))
	}
}

func TestFuseKeepsEveryResultExactlyOnce(t *testing.T) {
	got := search.Fuse(
		[]hit{"a", "b", "c"},
		[]hit{"b", "c", "d"},
	)

	if len(got) != 4 {
		t.Fatalf("got %d results (%s), want 4 distinct", len(got), keys(got))
	}

	seen := map[hit]bool{}
	for _, h := range got {
		if seen[h] {
			t.Errorf("%s appears twice in %s", h, keys(got))
		}
		seen[h] = true
	}
}

// One retriever returning nothing must not change the other's order. This is
// the case that runs whenever embeddings are unconfigured or the provider is
// down, which is to say most of the time for most installations.
func TestFuseWithOneEmptyListPreservesOrder(t *testing.T) {
	text := []hit{"first", "second", "third"}

	if got := search.Fuse(text, nil); keys(got) != "first,second,third" {
		t.Errorf("order = %s, want the original", keys(got))
	}
	if got := search.Fuse(nil, text); keys(got) != "first,second,third" {
		t.Errorf("order = %s, want the original", keys(got))
	}
}

func TestFuseIsDeterministic(t *testing.T) {
	text := []hit{"a", "b", "c", "d"}
	vector := []hit{"d", "c", "b", "a"}

	first := keys(search.Fuse(text, vector))
	for range 20 {
		if got := keys(search.Fuse(text, vector)); got != first {
			t.Fatalf("fusion returned %s then %s for identical input", first, got)
		}
	}
}

// A top result in one list beats something buried in the other.
//
// Note what this does *not* claim. Two deep appearances can outrank one top
// result — at k=60, twice 1/110 exceeds 1/61 — and that is the design rather
// than a flaw: agreement between two methods that fail differently is a real
// signal, and reciprocal rank fusion is meant to reward it. This test pins the
// weaker, uncontroversial half: one deep appearance is not enough.
func TestATopResultBeatsOneBuriedResult(t *testing.T) {
	var top []hit
	top = append(top, "top")
	for i := range 49 {
		top = append(top, hit("filler-a-"+string(rune('a'+i%26))+string(rune('0'+i/26))))
	}

	var other []hit
	for i := range 49 {
		other = append(other, hit("filler-b-"+string(rune('a'+i%26))+string(rune('0'+i/26))))
	}
	other = append(other, "buried")

	got := search.Fuse(top, other)

	if got[0] != "top" {
		t.Errorf("order starts %s; a first-place result should outrank one buried at position 50",
			keys(got[:min(3, len(got))]))
	}
}

func TestFuseOfNothingIsEmpty(t *testing.T) {
	if got := search.Fuse[hit](); len(got) != 0 {
		t.Errorf("got %d results from no lists", len(got))
	}
	if got := search.Fuse([]hit{}, []hit{}); len(got) != 0 {
		t.Errorf("got %d results from empty lists", len(got))
	}
}
