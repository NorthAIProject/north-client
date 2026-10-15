package progresschecks

import (
	"testing"

	progresspages "github.com/NorthAIProject/north-client/web/progress"
)

func TestLastPhotosTakesTheNewestOfEachPose(t *testing.T) {
	t.Parallel()
	checks := []progresspages.Check{
		{ID: "newest"}, // numbers only
		{ID: "middle", Photos: []progresspages.Photo{{Pose: "side", URL: "/side-2"}}},
		{ID: "oldest", Photos: []progresspages.Photo{{Pose: "front", URL: "/front-1"}, {Pose: "side", URL: "/side-1"}}},
	}
	last := lastPhotos(checks)
	if last == nil {
		t.Fatal("want photos to compare against")
	}
	if p, _ := last.Photo("front"); p.URL != "/front-1" {
		t.Errorf("front = %q", p.URL)
	}
	if p, _ := last.Photo("side"); p.URL != "/side-2" {
		t.Errorf("side = %q, want the newer one", p.URL)
	}
	if _, ok := last.Photo("back"); ok {
		t.Error("no back photo was ever taken")
	}
	if lastPhotos([]progresspages.Check{{ID: "x"}}) != nil {
		t.Error("no photos at all should be nil")
	}
}

func TestApplyEstimateFillsOnlyEmptyFields(t *testing.T) {
	t.Parallel()
	waist, hips := 95.0, 104.25
	form := applyEstimate(progresspages.Form{Hips: "106"}, Estimate{
		WaistCm: &waist, HipsCm: &hips, Confidence: "medium", Notes: "Chest hidden.",
	})
	if form.Waist != "95" || form.Hips != "106" || form.Chest != "" {
		t.Errorf("waist %q hips %q chest %q; want the estimate only where empty", form.Waist, form.Hips, form.Chest)
	}
	if form.Source != "assisted" || form.AssistConfidence != "medium" || form.AssistNotes != "Chest hidden." {
		t.Errorf("source %q confidence %q notes %q", form.Source, form.AssistConfidence, form.AssistNotes)
	}
	if n := form.Estimate; n == nil || n.Filled != 1 || n.Kept != 1 {
		t.Errorf("note = %+v, want 1 filled and 1 kept", n)
	}

	// Nothing filled leaves the check manual.
	none := applyEstimate(progresspages.Form{Waist: "90"}, Estimate{WaistCm: &waist, Confidence: "high"})
	if none.Source != "" || none.Estimate.Filled != 0 {
		t.Errorf("a typed number was overwritten or the check marked assisted: %+v", none)
	}
}

func TestFormInputCarriesAnAssistedSource(t *testing.T) {
	t.Parallel()
	in, err := formInput(progresspages.Form{Waist: "95", Source: "assisted", AssistConfidence: "low", AssistNotes: "Dim light."})
	if err != nil || in.Source != "assisted" || in.AssistConfidence != "low" || in.AssistNotes != "Dim light." {
		t.Fatalf("in = %+v, err %v", in, err)
	}
	manual, _ := formInput(progresspages.Form{Waist: "95", AssistConfidence: "high"})
	if manual.AssistConfidence != "" {
		t.Error("a manual check carried an assist confidence")
	}
}
