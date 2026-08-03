package analysis

import "testing"

func issue(seconds float64, severity, observation string) FormIssue {
	return FormIssue{
		TimestampSeconds: seconds,
		Severity:         severity,
		Observation:      observation,
		Correction:       "do the opposite",
	}
}

// The single most important rule in this feature. A model that reports faults
// while admitting it could not see the movement is guessing, and a guessed
// fault sends someone away to fix a problem they do not have.
func TestLowConfidenceDropsEveryFinding(t *testing.T) {
	t.Parallel()

	got := Sanitise(FormAnalysis{
		Exercise:   "unclear",
		Confidence: ConfidenceLow,
		Summary:    "I could not see the lift clearly.",
		Issues: []FormIssue{
			issue(4, SeverityCritical, "the back rounds badly"),
			issue(9, SeverityModerate, "knees cave"),
		},
	})

	if len(got.Issues) != 0 {
		t.Fatalf("low confidence must produce no findings, got %d: %+v", len(got.Issues), got.Issues)
	}
	if got.Trustworthy() {
		t.Fatal("low confidence must not be treated as trustworthy")
	}
}

func TestUnknownConfidenceIsTreatedAsLow(t *testing.T) {
	t.Parallel()

	// A value outside the enum means the model went off-script. Failing safe
	// means assuming it could not see, not assuming it could.
	got := Sanitise(FormAnalysis{
		Confidence: "pretty sure honestly",
		Issues:     []FormIssue{issue(3, SeverityCritical, "something")},
	})

	if got.Confidence != ConfidenceLow {
		t.Fatalf("confidence = %q, want low", got.Confidence)
	}
	if len(got.Issues) != 0 {
		t.Fatal("an unrecognised confidence must drop findings")
	}
}

func TestHighConfidenceKeepsFindings(t *testing.T) {
	t.Parallel()

	got := Sanitise(FormAnalysis{
		Exercise:   "Back squat",
		Confidence: ConfidenceHigh,
		Summary:    "Solid depth, some forward lean.",
		Issues: []FormIssue{
			issue(4, SeverityModerate, "hips rise before the chest"),
			issue(11, SeverityMinor, "slight knee cave on the last rep"),
		},
	})

	if len(got.Issues) != 2 {
		t.Fatalf("expected both findings kept, got %d", len(got.Issues))
	}
	if !got.Trustworthy() || got.Clean() {
		t.Fatal("a readable video with findings is trustworthy and not clean")
	}
}

func TestCleanSetIsARealResult(t *testing.T) {
	t.Parallel()

	got := Sanitise(FormAnalysis{
		Exercise:   "Deadlift",
		Confidence: ConfidenceHigh,
		Summary:    "Nothing to change.",
	})

	// A set with nothing wrong must read as a clean set, not as a failure to
	// find anything, or the feature quietly pressures the model to invent.
	if !got.Clean() {
		t.Fatal("a readable video with no findings should count as clean")
	}
}

func TestFindingsWithoutAnObservationAreDropped(t *testing.T) {
	t.Parallel()

	got := Sanitise(FormAnalysis{
		Confidence: ConfidenceHigh,
		Issues: []FormIssue{
			issue(4, SeverityModerate, "hips rise early"),
			issue(7, SeverityMinor, "   "),
		},
	})

	if len(got.Issues) != 1 {
		t.Fatalf("an empty observation should be dropped, got %d findings", len(got.Issues))
	}
}

func TestUnknownSeverityBecomesMinor(t *testing.T) {
	t.Parallel()

	got := Sanitise(FormAnalysis{
		Confidence: ConfidenceMedium,
		Issues:     []FormIssue{issue(2, "CATASTROPHIC", "the bar path drifts")},
	})

	// Calling everything critical means nothing is; an unrecognised severity
	// must not be allowed to shout.
	if got.Issues[0].Severity != SeverityMinor {
		t.Fatalf("severity = %q, want minor", got.Issues[0].Severity)
	}
}

func TestNegativeTimestampIsClamped(t *testing.T) {
	t.Parallel()

	got := Sanitise(FormAnalysis{
		Confidence: ConfidenceHigh,
		Issues:     []FormIssue{issue(-3, SeverityMinor, "early lean")},
	})

	if got.Issues[0].TimestampSeconds != 0 {
		t.Fatalf("timestamp = %v, want 0", got.Issues[0].TimestampSeconds)
	}
}

func TestTimestampFormatting(t *testing.T) {
	t.Parallel()

	tests := map[float64]string{
		0:     "0:00",
		7.4:   "0:07",
		61:    "1:01",
		125.9: "2:05",
	}

	for seconds, want := range tests {
		if got := (FormIssue{TimestampSeconds: seconds}).Timestamp(); got != want {
			t.Errorf("%.1fs formatted as %q, want %q", seconds, got, want)
		}
	}
}

func TestSchemaRequiresConfidenceBeforeIssues(t *testing.T) {
	t.Parallel()

	schema := Schema()

	// The model states whether it can see the movement before it starts listing
	// faults in it, which is what makes the low-confidence answer available at
	// all rather than rationalised after the fact.
	order := schema.PropertyOrdering
	confidenceAt, issuesAt := -1, -1
	for i, name := range order {
		switch name {
		case "confidence":
			confidenceAt = i
		case "issues":
			issuesAt = i
		}
	}

	if confidenceAt == -1 || issuesAt == -1 {
		t.Fatalf("schema is missing confidence or issues: %v", order)
	}
	if confidenceAt > issuesAt {
		t.Fatalf("confidence must be generated before issues, got %v", order)
	}
}
