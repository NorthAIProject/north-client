package plan

// MuscleGroups are the canonical muscle keys the 3D viewer can highlight
// (NOR-8). This is the Go-side source of truth for PlanSchema()'s enum
// constraint on Exercise.Primary/Secondary/Stabilizers.
//
// This list must stay in sync with two client-side copies, in this order
// when adding a new key:
//  1. MUSCLE_ALIASES in web/assets/js/shared/muscle-viewer/muscles.js — maps
//     the key to the body.glb mesh names it colours.
//  2. MUSCLE_INFO in the same file — the display name + description shown
//     in the click-to-inspect panel.
//  3. This slice.
//
// A key present here but missing from MUSCLE_ALIASES highlights nothing
// (viewer.js's setLoads silently skips unresolved keys); a key present in
// MUSCLE_ALIASES but missing here can never be produced by the AI schema.
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

// UnmodelledGroups are canonical keys the 3D model cannot draw yet.
//
// Empty as of the pec mapping: body.glb now keeps the Z-Anatomy pectoralis
// heads under the "chest" key. Keep the slice (and IsUnmodelled) so a future
// hole in the atlas has somewhere to be recorded without inventing a new
// apology path.
var UnmodelledGroups = []string{}

var unmodelledSet = func() map[string]bool {
	set := make(map[string]bool, len(UnmodelledGroups))
	for _, key := range UnmodelledGroups {
		set[key] = true
	}
	return set
}()

// IsUnmodelled reports whether key is a valid muscle group that body.glb
// cannot currently highlight.
func IsUnmodelled(key string) bool {
	return unmodelledSet[key]
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
