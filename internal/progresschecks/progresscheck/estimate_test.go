package progresscheck_test

import (
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/progresschecks/progresscheck"
)

func TestEstimateCleanDropsWhatASaveWouldRefuse(t *testing.T) {
	t.Parallel()

	got := progresscheck.Estimate{
		WaistCm: ptr(96.44), HipsCm: ptr(0), ChestCm: ptr(900),
		Confidence: " Medium ", Notes: "  Chest   hidden by\narms. ",
	}.Clean()

	if got.WaistCm == nil || *got.WaistCm != 96.4 {
		t.Errorf("waist = %v, want 96.4", got.WaistCm)
	}
	if got.HipsCm != nil {
		t.Errorf("zero is not visible, got hips %v", *got.HipsCm)
	}
	if got.ChestCm != nil {
		t.Errorf("implausible chest kept: %v", *got.ChestCm)
	}
	if got.Confidence != "medium" {
		t.Errorf("confidence = %q", got.Confidence)
	}
	if got.Notes != "Chest hidden by arms." {
		t.Errorf("notes = %q", got.Notes)
	}
}

func TestEstimateCleanWithNothingVisibleIsLow(t *testing.T) {
	t.Parallel()

	got := progresscheck.Estimate{WaistCm: ptr(0), Confidence: "high", Notes: strings.Repeat("x", 500)}.Clean()
	if !got.Empty() {
		t.Fatalf("want empty, got %+v", got)
	}
	if got.Confidence != "low" {
		t.Errorf("an empty estimate cannot be confident, got %q", got.Confidence)
	}
	if n := len([]rune(got.Notes)); n > 200 {
		t.Errorf("notes %d runes, want at most 200", n)
	}
}

func TestEstimateCleanUnknownConfidenceIsLow(t *testing.T) {
	t.Parallel()

	if got := (progresscheck.Estimate{WaistCm: ptr(90), Confidence: "certain"}).Clean(); got.Confidence != "low" {
		t.Errorf("confidence = %q", got.Confidence)
	}
}
