package caffeine

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestActiveHalvesEveryHalfLife(t *testing.T) {
	at := time.Date(2026, 9, 26, 8, 0, 0, 0, time.UTC)
	entries := []Entry{{MG: 100, LoggedAt: at}}

	if got := Active(entries, at); got != 100 {
		t.Errorf("at drinking time = %v, want 100", got)
	}
	if got := Active(entries, at.Add(HalfLife)); math.Abs(got-50) > 0.001 {
		t.Errorf("one half-life on = %v, want 50", got)
	}
	if got := Active(entries, at.Add(-time.Hour)); got != 0 {
		t.Errorf("before the drink = %v, want 0", got)
	}
}

func TestSummaryAndTotals(t *testing.T) {
	at := time.Date(2026, 9, 26, 8, 0, 0, 0, time.UTC)
	entries := []Entry{{MG: 100, LoggedAt: at}, {MG: 63, LoggedAt: at.Add(2 * time.Hour)}}
	if Total(entries) != 163 {
		t.Errorf("total = %d", Total(entries))
	}
	s := Summary(entries, at.Add(4*time.Hour))
	if !strings.Contains(s, "163mg") || !strings.Contains(s, "last at 10:00") {
		t.Errorf("summary = %q", s)
	}
	if Summary(nil, at) != "Caffeine: none logged today." {
		t.Error("an empty day should say so rather than stay silent")
	}
	if mg, ok := PresetMG("coffee"); !ok || mg != 100 {
		t.Error("coffee preset missing")
	}
}
