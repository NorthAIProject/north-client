package insights

import (
	"testing"
	"time"
)

func TestAreaWindows(t *testing.T) {
	lisbon, _ := time.LoadLocation("Europe/Lisbon")
	now := time.Date(2026, 10, 2, 9, 0, 0, 0, lisbon) // a Friday
	w := areaWindows(now, 3)
	if len(w) != 3 {
		t.Fatalf("got %d windows", len(w))
	}
	if got := w[0].Since.Format(time.DateOnly); got != "2026-09-14" {
		t.Fatalf("oldest starts %s", got)
	}
	if !w[1].Until.Equal(w[2].Since) || w[2].Since.Format(time.DateOnly) != "2026-09-28" {
		t.Fatalf("weeks do not join: %v / %v", w[1].Until, w[2].Since)
	}
	if !w[2].Until.Equal(now) {
		t.Fatalf("the current week ends at %v, want now", w[2].Until)
	}
	if len(areaWindows(now, 40)) != AreaWeeksDefault || len(areaWindows(now, 0)) != AreaWeeksDefault {
		t.Fatal("out-of-range weeks did not fall back to the default")
	}
}
