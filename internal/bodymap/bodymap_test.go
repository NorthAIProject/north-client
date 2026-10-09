package bodymap_test

import (
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/bodymap"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
)

func TestEveryMuscleGroupHasAPlaceOnTheFigure(t *testing.T) {
	t.Parallel()
	for _, key := range plan.MuscleGroups {
		if _, ok := bodymap.RegionOf(key); !ok {
			t.Errorf("%q is a muscle group with no region and no fold — its training would never show", key)
		}
	}
	for _, key := range bodymap.Regions {
		if !plan.IsMuscleGroup(key) {
			t.Errorf("region %q is not a muscle group", key)
		}
	}
	for from, to := range bodymap.Fold {
		if bodymap.IsRegion(from) || !bodymap.IsRegion(to) {
			t.Errorf("fold %q → %q must go from a non-region to a region", from, to)
		}
	}
}

func TestProjectListsEveryRegionAndFoldsHidden(t *testing.T) {
	t.Parallel()
	monday := time.Date(2026, 10, 5, 18, 0, 0, 0, time.UTC)
	tuesday := monday.AddDate(0, 0, 1)
	got := bodymap.Project([]lift.MuscleHeat{
		{Muscle: "traps", Intensity: 0.2, LastTrained: tuesday},
		{Muscle: "rhomboids", Intensity: 0.7, LastTrained: monday},
		{Muscle: "quads", Intensity: 0.5, LastTrained: monday},
		{Muscle: "not-a-muscle", Intensity: 1, LastTrained: monday},
	})
	if len(got) != len(bodymap.Regions) {
		t.Fatalf("got %d regions, want all %d", len(got), len(bodymap.Regions))
	}
	byRegion := map[string]bodymap.RegionHeat{}
	for _, r := range got {
		byRegion[r.Region] = r
	}
	if traps := byRegion["traps"]; traps.Intensity != 0.7 || !traps.LastTrained.Equal(tuesday) {
		t.Errorf("traps = %+v, want the rhomboids' heat and the later date", traps)
	}
	if chest := byRegion["chest"]; chest.Intensity != 0 || !chest.LastTrained.IsZero() {
		t.Errorf("untrained chest = %+v", chest)
	}
}
