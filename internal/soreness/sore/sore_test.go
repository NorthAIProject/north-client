package sore

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

// The body view lights muscles by key. A key the model does not know lights
// nothing, silently — so every mapping is checked against the model's list.
func TestEveryRegionMapsToRealMuscles(t *testing.T) {
	known := map[string]bool{}
	for _, k := range plan.MuscleGroups {
		known[k] = true
	}
	for _, region := range Regions() {
		muscles, ok := Muscles[region]
		if !ok {
			t.Errorf("region %q has no muscle mapping (use nil for none)", region)
		}
		for _, m := range muscles {
			if !known[m] {
				t.Errorf("region %q maps to %q, which the 3D body does not have", region, m)
			}
		}
	}
}

func TestSummary(t *testing.T) {
	got := Summary([]Entry{{Region: "lower_back", Severity: Sore}, {Region: "quads", Severity: Stiff}})
	if got != "Soreness today: lower back sore, quads stiff." {
		t.Errorf("summary = %q", got)
	}
	if Summary(nil) != "" {
		t.Error("no soreness is no line")
	}
}
