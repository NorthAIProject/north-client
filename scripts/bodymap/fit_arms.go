package main

import (
	"log"
	"math"
)

// The skin's arms were folded down from a T-pose rigidly, and the atlas arms
// hang in their own pose, so after the whole-body fit each arm's muscles sit
// a little outside the skin's arm. Each arm is therefore fitted on its own:
// turned about its shoulder and nudged, by the same misfit score.

// armKeys are the muscles that move with the arm. The deltoid wraps the
// shoulder joint, so it stays with the torso and marks where the arm pivots.
var armKeys = map[string]bool{"biceps": true, "triceps": true, "forearms": true}

// armPose turns an arm about its shoulder, abduction (about z) then flexion
// (about x), and shifts it.
type armPose struct {
	pivot        vec3
	abduct, flex float64
	shift        vec3
}

func (a armPose) apply(p vec3) vec3 {
	d := p.sub(a.pivot)
	cz, sz := math.Cos(a.abduct), math.Sin(a.abduct)
	d = vec3{d[0]*cz - d[1]*sz, d[0]*sz + d[1]*cz, d[2]}
	cx, sx := math.Cos(a.flex), math.Sin(a.flex)
	d = vec3{d[0], d[1]*cx - d[2]*sx, d[1]*sx + d[2]*cx}
	return a.pivot.add(d).add(a.shift)
}

// fitArms refits each arm in place and reports what it did.
func fitArms(field distanceField, muscles []triangle, keys []string) {
	for _, side := range []struct {
		name string
		sign float64
	}{{"left", 1}, {"right", -1}} {
		var arm []int
		var shoulder vec3
		var delts int
		for i, t := range muscles {
			c := t.centroid()
			if c[0]*side.sign <= 0 {
				continue
			}
			if armKeys[keys[i]] {
				arm = append(arm, i)
			} else if keys[i] == "delts" {
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
		pose := armPose{pivot: shoulder.scale(1 / float64(delts))}
		best := field.misfit(samples, pose.apply)
		start := best
		params := []func(*armPose) *float64{
			func(a *armPose) *float64 { return &a.abduct },
			func(a *armPose) *float64 { return &a.flex },
			func(a *armPose) *float64 { return &a.shift[0] },
			func(a *armPose) *float64 { return &a.shift[1] },
			func(a *armPose) *float64 { return &a.shift[2] },
		}
		// Angles in radians, shifts in metres: one step table serves both.
		for _, step := range []float64{0.04, 0.02, 0.01, 0.005, 0.0025} {
			for improved := true; improved; {
				improved = false
				for _, param := range params {
					for _, dir := range []float64{-1, 1} {
						trial := pose
						*param(&trial) += dir * step
						if c := field.misfit(samples, trial.apply); c < best {
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
		log.Printf("arm fit: %s abduct %+.1f° flex %+.1f° shift %+.0f %+.0f %+.0f mm, cost %.6f → %.6f",
			side.name, pose.abduct*180/math.Pi, pose.flex*180/math.Pi,
			pose.shift[0]*1000, pose.shift[1]*1000, pose.shift[2]*1000, start, best)
	}
}

// pushInside moves any muscle vertex still outside the skin to just under its
// nearest skin point, so no muscle pokes through the body. A vertex farther
// out than pushLimit is left alone: that is a fit problem to look at, not one
// to hide. Welding later keeps the moved copies of a vertex together, since
// every copy moves the same way.
func pushInside(field distanceField, skin mesh, muscles []triangle) {
	tris := make([]triangle, len(skin.tris))
	for i := range skin.tris {
		tris[i] = skin.triangle(i)
	}
	idx := newMuscleIndex(tris, make([]string, len(tris)), fitPad)
	var moved, left int
	for i, t := range muscles {
		for k, p := range t {
			if field.at(p) <= 0 {
				continue
			}
			_, c, ok := idx.closest(p, fitPad)
			if !ok || p.sub(c).length() > pushLimit {
				left++
				continue
			}
			muscles[i][k] = c.sub(p.sub(c).normalized().scale(pushDepth))
			moved++
		}
	}
	log.Printf("push inside: %d vertices moved under the skin, %d left outside (farther than %.0f mm)", moved, left, pushLimit*1000)
}

const (
	pushDepth = 0.002 // metres under the skin a pushed vertex ends up
	pushLimit = 0.02  // metres: farther out than this is left as a visible misfit
)
