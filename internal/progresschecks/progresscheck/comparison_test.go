package progresscheck_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/progresschecks/progresscheck"
)

func TestComparisonClean(t *testing.T) {
	t.Parallel()

	got, ok := progresscheck.Comparison{
		Summary:    "  Waist looks   narrower\nfrom the side. ",
		Confidence: "MEDIUM",
		Limits:     strings.Repeat("Different light. ", 40),
	}.Clean()
	if !ok || got.Summary != "Waist looks narrower from the side." || got.Confidence != "medium" {
		t.Errorf("got %+v, ok %v", got, ok)
	}
	if n := len([]rune(got.Limits)); n > 280 {
		t.Errorf("limits %d runes, want at most 280", n)
	}

	if _, ok := (progresscheck.Comparison{Summary: "  ", Confidence: "high"}).Clean(); ok {
		t.Error("an empty summary is not a comparison")
	}
	if got, _ := (progresscheck.Comparison{Summary: "x", Confidence: "sure"}).Clean(); got.Confidence != "low" {
		t.Errorf("unknown confidence = %q, want low", got.Confidence)
	}
}

func TestComparisonStale(t *testing.T) {
	t.Parallel()
	latest := uuid.New()
	c := progresscheck.Comparison{ToCheckID: latest}
	if c.Stale(latest) {
		t.Error("compared against the newest check is not stale")
	}
	if !c.Stale(uuid.New()) {
		t.Error("a newer check with photos makes it stale")
	}
}

func TestComparisonCoachLineIsLabelledAnObservation(t *testing.T) {
	t.Parallel()
	line := progresscheck.Comparison{
		FromOn: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), ToOn: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC),
		Summary: "Waist looks narrower from the side.", Confidence: "medium", Limits: "Different light.",
	}.CoachLine()
	for _, want := range []string{"1 Sep 2026", "15 Oct 2026", "observation from photos", "not a measurement", "medium confidence", "Waist looks narrower", "Limits: Different light."} {
		if !strings.Contains(line, want) {
			t.Errorf("coach line lacks %q: %s", want, line)
		}
	}
}
