// Package bodymap is the body figure's vocabulary: which muscle keys the figure
// has a patch of skin for, and how the rest fold into them.
//
// The figure (web/assets/models/body-map.glb, and BodyMap.usdz on iOS) is the
// skin of a body cut into one region per muscle key, built by scripts/bodymap.
// A muscle that never reaches the surface — the rhomboids sit under the
// trapezius — has no skin of its own, so its heat shows on the region above it.
// Clients only ever see region keys; the folding happens here, once.
package bodymap

import (
	"sort"
	"time"

	"github.com/NorthAIProject/north-client/internal/lifts/lift"
)

// Regions are the muscle keys the figure has skin for. scripts/bodymap fails
// if the asset and this list disagree.
var Regions = []string{
	"abs",
	"adductors",
	"biceps",
	"calves",
	"chest",
	"delts",
	"erectors",
	"forearms",
	"glutes",
	"hamstrings",
	"lats",
	"neck",
	"quads",
	"traps",
	"triceps",
}

// Fold maps a muscle key with no skin of its own to the region drawn over it.
// The rhomboids lie under the trapezius; serratus anterior is a thin strip on
// the side of the ribs, next to the external oblique that "abs" already covers.
var Fold = map[string]string{
	"rhomboids": "traps",
	"serratus":  "abs",
}

var regionSet = func() map[string]bool {
	set := make(map[string]bool, len(Regions))
	for _, key := range Regions {
		set[key] = true
	}
	return set
}()

// IsRegion reports whether key has its own patch of skin on the figure.
func IsRegion(key string) bool { return regionSet[key] }

// RegionOf is the region a muscle key shows on, and false for a key the
// figure does not know.
func RegionOf(muscle string) (string, bool) {
	if regionSet[muscle] {
		return muscle, true
	}
	region, ok := Fold[muscle]
	return region, ok
}

// RegionHeat is one region of the figure: how much it was trained recently, 0
// to 1, and when it was last trained at all (zero if never).
type RegionHeat struct {
	Region      string
	Intensity   float64
	LastTrained time.Time
}

// Project turns per-muscle heat into one entry per region, every region
// included and in Regions order. A region showing several muscles takes the
// hottest of them and the most recent date.
func Project(heat []lift.MuscleHeat) []RegionHeat {
	byRegion := make(map[string]RegionHeat, len(Regions))
	for _, h := range heat {
		region, ok := RegionOf(h.Muscle)
		if !ok {
			continue
		}
		r := byRegion[region]
		if h.Intensity > r.Intensity {
			r.Intensity = h.Intensity
		}
		if h.LastTrained.After(r.LastTrained) {
			r.LastTrained = h.LastTrained
		}
		byRegion[region] = r
	}
	out := make([]RegionHeat, 0, len(Regions))
	for _, key := range Regions {
		r := byRegion[key]
		r.Region = key
		out = append(out, r)
	}
	return out
}

// Map is the figure's whole payload: every region's heat over the last Days
// days, and when the person last trained at all.
type Map struct {
	Days        int
	LastSession time.Time // zero when no working set was ever logged
	Regions     []RegionHeat
}

// FromHeat projects per-muscle heat onto the figure.
func FromHeat(h lift.HeatMap) Map {
	return Map{Days: h.Days, LastSession: h.LastSession, Regions: Project(h.Muscles)}
}

// Hottest is the regions with any heat, hottest first, for summaries such as
// an accessibility label.
func Hottest(regions []RegionHeat) []RegionHeat {
	var out []RegionHeat
	for _, r := range regions {
		if r.Intensity > 0 {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Intensity > out[j].Intensity })
	return out
}
