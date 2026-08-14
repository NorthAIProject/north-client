package dashboard

import "testing"

func TestComputeDeltaWithoutPriorReportsNothing(t *testing.T) {
	// The case that matters: someone in their first week. Showing "+100%"
	// here would be inventing a comparison that does not exist.
	d := computeDelta(1200, 0)

	if d.HasPrior {
		t.Fatal("a zero prior is not something to compare against")
	}
	if d.Pct != 0 {
		t.Errorf("Pct = %v, want 0", d.Pct)
	}
	if d.Direction != 0 {
		t.Errorf("Direction = %d, want 0", d.Direction)
	}
	if d.Current != 1200 {
		t.Errorf("Current = %v, want 1200", d.Current)
	}
}

func TestComputeDeltaBothZero(t *testing.T) {
	d := computeDelta(0, 0)
	if d.HasPrior || d.Direction != 0 || d.Pct != 0 {
		t.Fatalf("nothing to nothing is no change: %+v", d)
	}
}

func TestComputeDeltaDirectionsAndPercent(t *testing.T) {
	tests := []struct {
		name           string
		current, prior float64
		wantPct        float64
		wantDir        int
	}{
		{"rise", 110, 100, 10, 1},
		{"fall", 90, 100, -10, -1},
		{"flat", 100, 100, 0, 0},
		{"doubled", 200, 100, 100, 1},
		{"collapsed to nothing", 0, 100, -100, -1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := computeDelta(tc.current, tc.prior)
			if !d.HasPrior {
				t.Fatal("expected a comparison")
			}
			if d.Pct != tc.wantPct {
				t.Errorf("Pct = %v, want %v", d.Pct, tc.wantPct)
			}
			if d.Direction != tc.wantDir {
				t.Errorf("Direction = %d, want %d", d.Direction, tc.wantDir)
			}
		})
	}
}

// A negative prior must not flip the sign of the percentage. No metric on the
// dashboard goes negative today, but the arithmetic should not be a trap for
// the first one that does.
func TestComputeDeltaNegativePriorKeepsDirectionHonest(t *testing.T) {
	d := computeDelta(-5, -10)
	if d.Direction != 1 {
		t.Errorf("Direction = %d, want 1: -5 is more than -10", d.Direction)
	}
	if d.Pct != 50 {
		t.Errorf("Pct = %v, want 50", d.Pct)
	}
}
