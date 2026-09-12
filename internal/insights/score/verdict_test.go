package score

import "testing"

func TestVerdictBands(t *testing.T) {
	// Boundaries are the whole risk in a banding function, so every one of
	// them is named here rather than sampled in the middle of a band.
	tests := []struct {
		points int
		want   Verdict
	}{
		{points: 100, want: VerdictStrong},
		{points: 85, want: VerdictStrong},
		{points: 84, want: VerdictOK},
		{points: 70, want: VerdictOK},
		{points: 69, want: VerdictUneven},
		{points: 50, want: VerdictUneven},
		{points: 49, want: VerdictLow},
		{points: 0, want: VerdictLow},
	}

	for _, tc := range tests {
		s := Score{Points: tc.points, HasData: true}
		if got := s.Verdict(); got != tc.want {
			t.Errorf("Score{Points: %d}.Verdict() = %q, want %q", tc.points, got, tc.want)
		}
	}
}

func TestVerdictIsUnknownWithoutData(t *testing.T) {
	// A domain with nothing logged must not read as "Low". Low is a claim
	// about the person; unknown is a claim about the data.
	s := Score{Points: 0, HasData: false}
	if got := s.Verdict(); got != VerdictUnknown {
		t.Errorf("Verdict() = %q, want %q", got, VerdictUnknown)
	}
}

func TestVerdictKeyIsNamespaced(t *testing.T) {
	s := Score{Points: 90, HasData: true}
	if got := s.Verdict().Key(); got != "score.verdict.strong" {
		t.Errorf("Key() = %q, want %q", got, "score.verdict.strong")
	}
}
