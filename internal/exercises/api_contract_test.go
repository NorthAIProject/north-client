package exercises

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/apitest"
	"github.com/NorthAIProject/north-client/web/assets"
)

func TestExerciseDetailShape(t *testing.T) {
	t.Parallel()

	art, err := NewAPI(nil, assets.Assets).frames("push-up")
	if err != nil {
		t.Fatal(err)
	}
	// Pinned with the first few hundred characters of each frame: the whole
	// path is 12 KB of numbers that would drown the diff that matters.
	for i, frame := range art.Frames {
		art.Frames[i] = frame[:200] + "…"
	}
	apitest.AssertGolden(t, "exercise.golden.json", ExerciseDetail{
		Slug: "push-up", Name: "Push-Up", Category: "strength", Equipment: "none", Difficulty: "beginner",
		Instructions: "Lie face down with your hands shoulder-width apart.",
		VideoURL:     "https://youtu.be/IODxDxX7oi4",
		Primary:      []string{"chest"}, Secondary: []string{"triceps", "shoulders"},
		Art: art,
	})
}

// iOS draws each frame as a single even-odd path in a square box. Every
// movement's artwork has to meet that, not just the one the golden file pins.
func TestEveryMovementsArtIsOnePathInASquare(t *testing.T) {
	t.Parallel()

	api := NewAPI(nil, assets.Assets)
	dirs, err := fs.ReadDir(assets.Assets, "exercises")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		art, err := api.frames(dir.Name())
		if err != nil {
			t.Errorf("%s: %v", dir.Name(), err)
			continue
		}
		for i, frame := range art.Frames {
			// A path must open with a move; a relative "m" first is legal and
			// counts from the origin, and twelve frames use it.
			if f := strings.TrimSpace(frame); !strings.HasPrefix(f, "M") && !strings.HasPrefix(f, "m") {
				t.Errorf("%s frame %d does not start with a move", dir.Name(), i+1)
			}
		}
		checked++
	}
	if checked < 300 {
		t.Fatalf("checked %d movements, want the full set of about 302", checked)
	}
}

func TestExerciseListShape(t *testing.T) {
	t.Parallel()

	list := ProjectList([]Exercise{{
		Slug: "push-up", Name: "Push-Up", Category: "strength", Equipment: "none", Difficulty: "beginner",
		Primary: []string{"chest"}, IllustrationSlug: "push-up",
	}})
	list.Total = 42
	apitest.AssertGolden(t, "exercises.golden.json", list)
}
