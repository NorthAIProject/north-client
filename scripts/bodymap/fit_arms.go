package main

import (
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
)

// The skin's arms were folded down from a T-pose rigidly, and the atlas arms
// hang in their own pose, so after the whole-body fit each arm's muscles sit
// a little outside the skin's arm. Each arm is therefore fitted on its own:
// turned about its shoulder and nudged, by the same misfit score.

// armKeys are the muscles of the arm itself. The deltoid wraps the shoulder
// joint: it marks where the arm pivots, and it moves with the arm, since this
// skin's shoulders are broader than the atlas's and a deltoid left behind
// would sit inside the chest.
var armKeys = map[string]bool{"biceps": true, "triceps": true, "forearms": true}

// armPose turns an arm about its shoulder, abduction (about z) then flexion
// (about x), and shifts it.
type armPose struct {
	pivot        vec3
	abduct, flex float64
	shift        vec3
}

// invert undoes apply.
func (a armPose) invert(p vec3) vec3 {
	d := p.sub(a.shift).sub(a.pivot)
	cx, sx := math.Cos(-a.flex), math.Sin(-a.flex)
	d = vec3{d[0], d[1]*cx - d[2]*sx, d[1]*sx + d[2]*cx}
	cz, sz := math.Cos(-a.abduct), math.Sin(-a.abduct)
	d = vec3{d[0]*cz - d[1]*sz, d[0]*sz + d[1]*cz, d[2]}
	return a.pivot.add(d)
}

func (a armPose) apply(p vec3) vec3 {
	d := p.sub(a.pivot)
	cz, sz := math.Cos(a.abduct), math.Sin(a.abduct)
	d = vec3{d[0]*cz - d[1]*sz, d[0]*sz + d[1]*cz, d[2]}
	cx, sx := math.Cos(a.flex), math.Sin(a.flex)
	d = vec3{d[0], d[1]*cx - d[2]*sx, d[1]*sx + d[2]*cx}
	return a.pivot.add(d).add(a.shift)
}

// handShare is how much of the skin's arm, from the fingertips up, is hand:
// no muscle of the figure's lies there, so it is not asked to be covered.
const handShare = 0.25

// fitArms refits each arm in place and reports what it did. armSkin is the
// skin of both arms, hands included; like the body, an arm is scored on its
// muscles staying inside its skin and on its skin having muscle under it.
func fitArms(field distanceField, skin mesh, muscles []triangle, keys []string, armSkin []skinSample) {
	for _, side := range []struct {
		name string
		sign float64
	}{{"left", 1}, {"right", -1}} {
		var arm []int
		var ownTris []triangle // the arm without its deltoid: what the arm's skin is covered by
		var shoulder vec3
		var delts int
		for i, t := range muscles {
			c := t.centroid()
			if c[0]*side.sign <= 0 {
				continue
			}
			if armKeys[keys[i]] {
				arm = append(arm, i)
				ownTris = append(ownTris, muscles[i])
			} else if keys[i] == "delts" {
				arm = append(arm, i)
				shoulder = shoulder.add(c)
				delts++
			}
		}
		if len(arm) == 0 || delts == 0 {
			log.Printf("arm fit: no %s arm in the atlas", side.name)
			continue
		}
		var samples []vec3
		for k, i := range arm {
			if k%3 == 0 {
				samples = append(samples, muscles[i][0])
			}
		}
		skinArm := forearmAndUpper(armSkin, side.sign)
		// A few hundred points of skin against a third of the arm's
		// triangles: the search scores hundreds of poses, and neither set
		// needs to be finer than the fit is.
		skinArm = thin(skinArm, 400)
		var sparse []triangle
		for k := 0; k < len(ownTris); k += 3 {
			sparse = append(sparse, ownTris[k])
		}
		cover := newCoverage(sparse, skinArm)
		score := func(a armPose) float64 { return field.misfit(samples, a.apply) + cover.cost(a.invert) }
		var from, to vec3
		for _, t := range ownTris {
			from = from.add(t.centroid())
		}
		from = from.scale(1 / float64(len(ownTris)))
		pose := armPose{pivot: shoulder.scale(1 / float64(delts))}
		start := score(pose)
		// The atlas arm can hang inside the skin's chest, with the skin of the
		// trunk between it and the skin's arm: no small step crosses that. So
		// the search starts with the arm moved sideways onto the skin's.
		for _, p := range skinArm {
			to = to.add(p.at)
		}
		if len(skinArm) > 0 {
			d := to.scale(1 / float64(len(skinArm))).sub(from)
			pose.shift = vec3{d[0], 0, d[2]}
		}
		params := []func(*armPose) *float64{
			func(a *armPose) *float64 { return &a.abduct },
			func(a *armPose) *float64 { return &a.flex },
			func(a *armPose) *float64 { return &a.shift[0] },
			func(a *armPose) *float64 { return &a.shift[1] },
			func(a *armPose) *float64 { return &a.shift[2] },
		}
		// Angles in radians, shifts in metres: one step table serves both.
		best := score(pose)
		for _, step := range []float64{0.04, 0.02, 0.01, 0.005, 0.0025} {
			for improved := true; improved; {
				improved = false
				for _, param := range params {
					for _, dir := range []float64{-1, 1} {
						trial := pose
						*param(&trial) += dir * step
						if c := score(trial); c < best {
							best, pose, improved = c, trial, true
						}
					}
				}
			}
		}
		for _, i := range arm {
			t := muscles[i]
			muscles[i] = triangle{pose.apply(t[0]), pose.apply(t[1]), pose.apply(t[2])}
		}
		// The deltoid goes into the sleeve with the arm: the sleeve runs on
		// over the shoulder.
		sleeve(muscles, arm, withShoulder(armSkinOf(armSkin, side.sign), skin, side.sign))
		log.Printf("arm fit: %s abduct %+.1f° flex %+.1f° shift %+.0f %+.0f %+.0f mm, cost %.6f → %.6f",
			side.name, pose.abduct*180/math.Pi, pose.flex*180/math.Pi,
			pose.shift[0]*1000, pose.shift[1]*1000, pose.shift[2]*1000, start, best)
	}
}

// thin keeps about n of the samples, evenly through the list, and gives each
// the area of those it stands for.
func thin(samples []skinSample, n int) []skinSample {
	if len(samples) <= n {
		return samples
	}
	stride := len(samples) / n
	var out []skinSample
	for i := 0; i < len(samples); i += stride {
		s := samples[i]
		for _, o := range samples[i+1 : min(i+stride, len(samples))] {
			s.area += o.area
		}
		out = append(out, s)
	}
	return out
}

// forearmAndUpper is one side's arm skin without the hand.
func forearmAndUpper(armSkin []skinSample, sign float64) []skinSample {
	lo, hi := math.Inf(1), math.Inf(-1)
	var side []skinSample
	for _, p := range armSkin {
		if p.at[0]*sign > 0 {
			side = append(side, p)
			lo, hi = math.Min(lo, p.at[1]), math.Max(hi, p.at[1])
		}
	}
	wrist := lo + handShare*(hi-lo)
	var out []skinSample
	for _, p := range side {
		if p.at[1] > wrist {
			out = append(out, p)
		}
	}
	return out
}

// pushInside moves any muscle vertex still outside the skin to just under its
// nearest skin point, so no muscle pokes through the body: the calves, say,
// bulge past this slimmer skin and come to hug it instead. A vertex farther
// out than pushLimit is left alone and logged by muscle: that is a fit problem
// to look at, not one to hide. Welding later keeps the moved copies of a
// vertex together, since every copy moves the same way.
func pushInside(field distanceField, skin mesh, muscles []triangle, keys []string) {
	idx := skinIndex(skin)
	var moved int
	left := map[string]int{}
	for i, t := range muscles {
		for k, p := range t {
			if field.at(p) <= 0 {
				continue
			}
			_, c, ok := idx.closest(p, fitPad)
			if !ok || p.sub(c).length() > pushLimit {
				left[keys[i]]++
				continue
			}
			muscles[i][k] = c.sub(p.sub(c).normalized().scale(pushDepth))
			moved++
		}
	}
	var where []string
	for key, n := range left {
		where = append(where, fmt.Sprintf("%s %d", key, n))
	}
	sort.Strings(where)
	log.Printf("push inside: %d vertices moved under the skin; left outside (farther than %.0f mm): %s", moved, pushLimit*1000, strings.Join(where, ", "))
}

// hug draws the muscles out to the skin. The skin is a fuller body than the
// atlas, so fitted muscle lies up to a few centimetres under it, and a figure
// drawn that way is a thin body standing inside a bigger one. Each vertex
// keeps its place along the line to its nearest skin point but its depth is
// compressed, most near the surface and not at all at the end of its reach,
// so the layers keep their order and nothing tears where the reach ends.
func hug(skin mesh, muscles []triangle) {
	tris := make([]triangle, len(skin.tris))
	for i := range skin.tris {
		tris[i] = skin.triangle(i)
	}
	idx := newTriangleIndex(tris, hugReach)
	var moved int
	for i, t := range muscles {
		for k, p := range t {
			_, c, ok := idx.closest(p, hugReach)
			if !ok {
				continue
			}
			in := p.sub(c)
			depth := in.length()
			if depth < pushDepth {
				continue
			}
			share := depth / hugReach
			to := pushDepth + depth*(hugShare+(1-hugShare)*share*share*share)
			if to >= depth {
				continue
			}
			muscles[i][k] = c.add(in.scale(to / depth))
			moved++
		}
	}
	log.Printf("hug: %d muscle vertices drawn out towards the skin", moved)
}

// trimTendons drops the long tendons that run on from the forearm into the
// fingers and from the lower leg into the toes: thin strings that never line
// up with this skin's hands and feet. wrist and ankle are heights.
func trimTendons(muscles []triangle, keys []string, wrist, ankle float64) ([]triangle, []string) {
	var outT []triangle
	var outK []string
	for i, t := range muscles {
		y := t.centroid()[1]
		if (keys[i] == "forearms" && y < wrist) || (keys[i] == "calves" && y < ankle) {
			continue
		}
		outT = append(outT, t)
		outK = append(outK, keys[i])
	}
	log.Printf("trim: %d tendon triangles dropped below the wrists and ankles", len(muscles)-len(outT))
	return outT, outK
}

// wristHeight is where the skin's hands begin.
func wristHeight(armSkin []skinSample) float64 {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, p := range armSkin {
		lo, hi = math.Min(lo, p.at[1]), math.Max(hi, p.at[1])
	}
	return lo + handShare*(hi-lo)
}

const (
	hugShare  = 0.12   // how much of its depth a muscle just under the skin keeps
	hugReach  = 0.08   // metres under the skin beyond which a muscle is left where it is
	pushDepth = 0.002  // metres under the skin a pushed vertex ends up
	pushLimit = fitPad // metres: farther out than this is left as a visible misfit
)
