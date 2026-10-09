package plan

// MuscleGroups are the canonical muscle keys (NOR-8). This is the Go-side
// source of truth for PlanSchema()'s enum constraint on
// Exercise.Primary/Secondary/Stabilizers.
//
// Adding a key touches, in this order:
//  1. MUSCLE_ALIASES in web/assets/js/shared/muscle-viewer/muscles.js — the
//     atlas mesh names that become the key's patch of skin when
//     scripts/bodymap rebuilds body-map.glb.
//  2. MUSCLE_INFO in the same file — the display name and description.
//  3. This slice.
//  4. internal/bodymap: Regions if the key got skin, else Fold. Its tests
//     fail until every key here has a place on the figure.
var MuscleGroups = []string{
	"quads",
	"glutes",
	"hamstrings",
	"calves",
	"adductors",
	"traps",
	"delts",
	"biceps",
	"triceps",
	"forearms",
	"lats",
	"rhomboids",
	"erectors",
	"serratus",
	"abs",
	"chest",
	"neck",
}

var muscleGroupSet = func() map[string]bool {
	set := make(map[string]bool, len(MuscleGroups))
	for _, key := range MuscleGroups {
		set[key] = true
	}
	return set
}()

// IsMuscleGroup reports whether key is one of the canonical groups above.
func IsMuscleGroup(key string) bool {
	return muscleGroupSet[key]
}
