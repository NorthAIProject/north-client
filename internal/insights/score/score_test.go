package score

import "testing"

func TestNewScoresOverKnownComponentsOnly(t *testing.T) {
	tests := []struct {
		name         string
		components   []Component
		wantPoints   int
		wantCoverage int
		wantHasData  bool
	}{
		{
			name: "every component known scores over the full weight",
			components: []Component{
				{Key: "a", Earned: 20, Weight: 40, Known: true},
				{Key: "b", Earned: 30, Weight: 60, Known: true},
			},
			wantPoints:   50,
			wantCoverage: 100,
			wantHasData:  true,
		},
		{
			// The whole point: an unmeasured component must not drag the
			// score down. Apple shows "no data", never a zero.
			name: "an unknown component leaves the ratio of the rest alone",
			components: []Component{
				{Key: "a", Earned: 20, Weight: 40, Known: true},
				{Key: "b", Earned: 0, Weight: 60, Known: false},
			},
			wantPoints:   50,
			wantCoverage: 40,
			wantHasData:  true,
		},
		{
			name: "coverage below the floor reports no data rather than a number",
			components: []Component{
				{Key: "a", Earned: 10, Weight: 30, Known: true},
				{Key: "b", Earned: 0, Weight: 70, Known: false},
			},
			wantPoints:   0,
			wantCoverage: 30,
			wantHasData:  false,
		},
		{
			name: "nothing known does not divide by zero",
			components: []Component{
				{Key: "a", Earned: 0, Weight: 40, Known: false},
				{Key: "b", Earned: 0, Weight: 60, Known: false},
			},
			wantPoints:   0,
			wantCoverage: 0,
			wantHasData:  false,
		},
		{
			name:         "no components at all is not a crash",
			components:   nil,
			wantPoints:   0,
			wantCoverage: 0,
			wantHasData:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := New("body", tc.components)

			if got.Points != tc.wantPoints {
				t.Errorf("Points = %d, want %d", got.Points, tc.wantPoints)
			}
			if got.Coverage != tc.wantCoverage {
				t.Errorf("Coverage = %d, want %d", got.Coverage, tc.wantCoverage)
			}
			if got.HasData != tc.wantHasData {
				t.Errorf("HasData = %v, want %v", got.HasData, tc.wantHasData)
			}
			if got.Domain != "body" {
				t.Errorf("Domain = %q, want %q", got.Domain, "body")
			}
		})
	}
}
