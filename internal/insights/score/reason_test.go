package score

import "testing"

func TestReasonNamesTheBestAndWorstComponent(t *testing.T) {
	s := New("body", []Component{
		{Key: "sleep_duration", Earned: 40, Weight: 40, Known: true}, // 100%
		{Key: "hydration", Earned: 5, Weight: 25, Known: true},       // 20%
		{Key: "habits", Earned: 10, Weight: 15, Known: true},         // 67%
	})

	got, ok := s.Reason()
	if !ok {
		t.Fatal("Reason() returned ok=false, want a reason")
	}
	if got.Best != "sleep_duration" {
		t.Errorf("Best = %q, want %q", got.Best, "sleep_duration")
	}
	if got.Worst != "hydration" {
		t.Errorf("Worst = %q, want %q", got.Worst, "hydration")
	}
	if got.Key != "score.reason" {
		t.Errorf("Key = %q, want %q", got.Key, "score.reason")
	}
}

func TestReasonIgnoresUnknownComponents(t *testing.T) {
	// An unmeasured component is not the reason anything went wrong.
	s := New("body", []Component{
		{Key: "sleep_duration", Earned: 40, Weight: 40, Known: true},
		{Key: "hydration", Earned: 20, Weight: 25, Known: true},
		{Key: "bedtime", Earned: 0, Weight: 20, Known: false},
	})

	got, ok := s.Reason()
	if !ok {
		t.Fatal("Reason() returned ok=false, want a reason")
	}
	if got.Worst == "bedtime" || got.Best == "bedtime" {
		t.Errorf("unknown component named in reason: %+v", got)
	}
}

func TestReasonNeedsSomethingToContrast(t *testing.T) {
	tests := []struct {
		name       string
		components []Component
	}{
		{
			name: "a single known component has nothing to compare against",
			components: []Component{
				{Key: "sleep_duration", Earned: 30, Weight: 40, Known: true},
				{Key: "hydration", Earned: 0, Weight: 25, Known: false},
			},
		},
		{
			// Everything at the same ratio means nothing stood out. Naming an
			// arbitrary one would invent a story the numbers do not tell.
			name: "components that all scored alike have no standout",
			components: []Component{
				{Key: "sleep_duration", Earned: 20, Weight: 40, Known: true},
				{Key: "hydration", Earned: 30, Weight: 60, Known: true},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := New("body", tc.components).Reason(); ok {
				t.Error("Reason() returned ok=true, want no reason")
			}
		})
	}
}

func TestReasonWithoutDataIsNoReason(t *testing.T) {
	s := New("body", []Component{{Key: "hydration", Earned: 5, Weight: 25, Known: true}})
	if _, ok := s.Reason(); ok {
		t.Error("Reason() returned ok=true for a score with no data")
	}
}
