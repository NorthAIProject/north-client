package notifications

import "testing"

func TestValidateNormalisesAnEmptyCadence(t *testing.T) {
	// The column has a NOT NULL default, but a form that omitted the field
	// would post an empty string and the CHECK would reject it as a 500.
	in := Input{QuietStart: "22:00", QuietEnd: "07:00"}

	out, err := Validate(in)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if out.StatsDigestCadence != DefaultCadence {
		t.Errorf("cadence = %q, want %q", out.StatsDigestCadence, DefaultCadence)
	}
}

func TestValidateRejectsACadenceTheColumnWouldRefuse(t *testing.T) {
	// Hand-posted, or a stale form from a build that offered more choices.
	// Better a field error than a constraint violation surfacing as a 500.
	in := Input{StatsDigestCadence: "fortnightly", QuietStart: "22:00", QuietEnd: "07:00"}

	if _, err := Validate(in); err == nil {
		t.Fatal("an unknown cadence was accepted")
	}
}

func TestValidateKeepsEveryOfferedCadence(t *testing.T) {
	for _, c := range []string{CadenceOff, CadenceDaily, CadenceWeekly, CadenceMonthly} {
		t.Run(c, func(t *testing.T) {
			in := Input{StatsDigestCadence: c, QuietStart: "22:00", QuietEnd: "07:00"}

			out, err := Validate(in)
			if err != nil {
				t.Fatalf("validate: %v", err)
			}
			if out.StatsDigestCadence != c {
				t.Errorf("cadence = %q, want %q", out.StatsDigestCadence, c)
			}
		})
	}
}
