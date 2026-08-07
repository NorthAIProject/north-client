package activity

import "testing"

// METTable is hand-maintained and hand-extended, and every entry becomes a
// calorie figure someone is shown. These are the mistakes that a reading of
// the list would not catch.

func TestMETCodesAreUnique(t *testing.T) {
	t.Parallel()

	// A duplicate code is stored in activity_sessions.activity_code and later
	// looked up to name the session, so the second entry becomes unreachable
	// while still appearing in the picker.
	seen := map[string]string{}
	for _, met := range METTable {
		if previous, taken := seen[met.Code]; taken {
			t.Errorf("code %q is used by both %q and %q", met.Code, previous, met.Name)
			continue
		}
		seen[met.Code] = met.Name
	}
}

func TestMETNamesAreUnique(t *testing.T) {
	t.Parallel()

	// Two identical labels in the picker are indistinguishable to whoever is
	// choosing between them.
	seen := map[string]bool{}
	for _, met := range METTable {
		if seen[met.Name] {
			t.Errorf("two entries are both labelled %q", met.Name)
		}
		seen[met.Name] = true
	}
}

func TestMETValuesArePlausible(t *testing.T) {
	t.Parallel()

	// 1.0 is lying still; sustained efforts above 20 are beyond what these
	// entries describe. A value outside this range is a conversion error —
	// the ported entries were calories per hour before being divided by a
	// 70kg reference weight, and a missed division lands in the hundreds.
	for _, met := range METTable {
		if met.Value < 1.0 || met.Value > 20.0 {
			t.Errorf("%q has MET %v, outside the plausible 1.0-20.0 range", met.Code, met.Value)
		}
	}
}

func TestEveryMETEntryIsComplete(t *testing.T) {
	t.Parallel()

	categories := map[string]bool{"cardio": true, "strength": true, "flexibility": true, "sport": true}

	for _, met := range METTable {
		if met.Code == "" || met.Name == "" {
			t.Errorf("entry %+v is missing a code or name", met)
		}
		if !categories[met.Category] {
			t.Errorf("%q has category %q, which is not one of the four the UI groups by", met.Code, met.Category)
		}
	}
}
