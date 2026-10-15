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
