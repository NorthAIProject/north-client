package supplement

import "testing"

func TestCoverageCountsSupplementsAndOtherSources(t *testing.T) {
	entries := []Entry{
		{Name: "Omega-3", Nutrients: []string{"omega3"}},
		{Name: "D3", Nutrients: []string{"vitamin_d"}},
	}
	c := CoverageFor(entries, []string{"vitamin_c", "omega3"})
	if len(c.Covered) != 3 || len(c.Covered)+len(c.Missing) != len(Nutrients()) {
		t.Errorf("coverage = %+v", c)
	}
	if c.Covered[0] != "vitamin_c" {
		t.Errorf("lists should follow the catalogue order, got %v", c.Covered)
	}
}

func TestPresetsOnlyNameTrackedNutrients(t *testing.T) {
	for _, p := range Presets() {
		for _, n := range p.Nutrients {
			if !ValidNutrient(n) {
				t.Errorf("preset %s names untracked nutrient %q", p.Key, n)
			}
		}
	}
	if len(Nutrients()) != 15 {
		t.Errorf("the card promises fifteen, got %d", len(Nutrients()))
	}
}

func TestLabel(t *testing.T) {
	if (Entry{Name: "Omega-3", Count: 3}).Label() != "Omega-3 ×3" {
		t.Error("count missing from label")
	}
}
