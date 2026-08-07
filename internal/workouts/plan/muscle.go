package plan

// MuscleGroups are the canonical muscle keys the 3D viewer can highlight
// (NOR-8). This is the Go-side source of truth for PlanSchema()'s enum
// constraint on Exercise.Primary/Secondary/Stabilizers.
//
// This list must stay in sync with two client-side copies, in this order
// when adding a new key:
//  1. MUSCLE_ALIASES in web/assets/js/shared/muscle-viewer/viewer.js — maps
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
// body.glb ships no pectoralis major or minor mesh — all 231 of its node
// names were checked, and the "CHEST" node in the file is a camera-preset
// label alongside HEAD, HIP, and KNEE, not anatomy. So "chest" is a real
// muscle group that exercises may legitimately target, and the viewer has
// nothing to colour for it.
//
// Callers must say so rather than silently dropping the key: a bench press
// whose muscle list omits the chest reads as a data bug, not as a missing
// asset. Remove a key from here the moment the model gains its mesh.
var UnmodelledGroups = []string{"chest"}

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
