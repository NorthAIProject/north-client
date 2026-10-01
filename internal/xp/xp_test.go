package xp

import (
	"testing"
)

func TestLevelFor(t *testing.T) {
	t.Parallel()

	cases := []struct {
		total      int
		title      string
		floor, nxt int
	}{
		{0, "Starter", 0, 100},
		{99, "Starter", 0, 100},
		{100, "Mover", 100, 300},
		{799, "Regular", 300, 800},
		{6000, "Relentless", 6000, 0},
		{99999, "Relentless", 6000, 0},
	}
	for _, c := range cases {
		got := LevelFor(c.total)
		if got.Title != c.title || got.Floor != c.floor || got.Next != c.nxt {
			t.Errorf("LevelFor(%d) = %+v, want %s from %d to %d", c.total, got, c.title, c.floor, c.nxt)
		}
	}
}

// Equal values share a place and the next place skips; ties list by name.
func TestRank(t *testing.T) {
	t.Parallel()

	entries := []Entry{
		{DisplayName: "Cleo", Value: 40},
		{DisplayName: "Bo", Value: 90},
		{DisplayName: "Ana", Value: 40},
		{DisplayName: "Dan", Value: 10},
	}
	rank(entries)
	want := []struct {
		name string
		rank int
	}{{"Bo", 1}, {"Ana", 2}, {"Cleo", 2}, {"Dan", 4}}
	for i, w := range want {
		if entries[i].DisplayName != w.name || entries[i].Rank != w.rank {
			t.Fatalf("place %d = %s #%d, want %s #%d", i, entries[i].DisplayName, entries[i].Rank, w.name, w.rank)
		}
	}
}
