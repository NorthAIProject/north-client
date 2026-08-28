package exerciserow

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/exercises/exercise"
)

func illustrated() exercise.Exercise {
	return exercise.Exercise{
		Slug:             "bench-press",
		Name:             "Bench Press",
		Equipment:        "barbell",
		IllustrationSlug: "bench-press",
	}
}

func bodyweight() exercise.Exercise {
	return exercise.Exercise{
		Slug:      "push-up",
		Name:      "Push-Up",
		Equipment: exercise.EquipmentNone,
	}
}

func html(t *testing.T, e exercise.Exercise, which string) string {
	t.Helper()

	var b strings.Builder
	var err error
	switch which {
	case "thumbnail":
		err = Thumbnail(e, "pick-", "w-8").Render(context.Background(), &b)
	case "equipment":
		err = Equipment(e).Render(context.Background(), &b)
	}
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

// The id namespaces Alpine state per row. Two rows sharing one cycle their
// frames as though they were a single illustration.
func TestThumbnailComposesTheIDFromThePrefixAndSlug(t *testing.T) {
	t.Parallel()

	got := html(t, illustrated(), "thumbnail")

	if !strings.Contains(got, `id="pick-bench-press"`) {
		t.Errorf("id is not prefix+slug: %s", got)
	}
	if !strings.Contains(got, "/assets/exercises/bench-press/frame-1.svg") {
		t.Error("the artwork did not render")
	}
	if !strings.Contains(got, "w-8") {
		t.Error("the caller's size class was dropped")
	}
}

// Most of the catalog has no artwork, so an absent illustration is ordinary.
// Rendering an empty sized box would leave a gap in the row.
func TestThumbnailRendersNothingWithoutArtwork(t *testing.T) {
	t.Parallel()

	if got := html(t, bodyweight(), "thumbnail"); strings.TrimSpace(got) != "" {
		t.Errorf("rendered %q for an exercise with no artwork", got)
	}
}

func TestEquipmentLabelsWhatIsNeeded(t *testing.T) {
	t.Parallel()

	if got := html(t, illustrated(), "equipment"); !strings.Contains(got, "barbell") {
		t.Errorf("the equipment is not labelled: %s", got)
	}
}

// "none" is what the catalog stores for bodyweight movements. Printing it would
// put the word "none" in a row where the honest answer is silence.
func TestEquipmentSaysNothingForABodyweightMovement(t *testing.T) {
	t.Parallel()

	if got := html(t, bodyweight(), "equipment"); strings.TrimSpace(got) != "" {
		t.Errorf("rendered %q for a movement that needs nothing", got)
	}
}
