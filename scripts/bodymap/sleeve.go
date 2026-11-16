package main

import (
	"log"
	"math"
)

// This skin's arms are not the atlas's arms made bigger: the upper arm is
// about twice as thick and the forearm hardly thicker at all. No one pose of
// a rigid arm fills that sleeve; it lies against one side and leaves the
// other bare. So once an arm is posed, its muscles are fitted to the sleeve
// slice by slice: at each height they are centred on the skin's arm and
// stretched outward, each direction by as much as it takes for the outermost
// muscle there to reach the skin.

const (
	sleeveSlice  = 0.01  // metres of height per slice
	sleeveAngles = 16    // directions round the arm
	sleeveGap    = 0.003 // metres the stretched muscle stops short of the skin
	sleeveBlend  = 0.06  // metres below the top of the sleeve over which the fit eases in
	sleeveMost   = 2.6   // the most a direction is stretched
	sleeveLeast  = 0.7   // and the most it is shrunk
)

// ring is one slice of an arm: its centre and how far it reaches from there
// in each direction.
type ring struct {
	centre [2]float64
	radius [sleeveAngles]float64
	filled bool
}

// rings slices points by height, from y = lo upward.
func rings(points []vec3, lo float64, n int) []ring {
	type box struct{ lx, hx, lz, hz float64 }
	boxes := make([]box, n)
	for i := range boxes {
		boxes[i] = box{math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)}
	}
	slice := func(p vec3) int { return int((p[1] - lo) / sleeveSlice) }
	for _, p := range points {
		if b := slice(p); b >= 0 && b < n {
			boxes[b].lx, boxes[b].hx = math.Min(boxes[b].lx, p[0]), math.Max(boxes[b].hx, p[0])
			boxes[b].lz, boxes[b].hz = math.Min(boxes[b].lz, p[2]), math.Max(boxes[b].hz, p[2])
		}
	}
	out := make([]ring, n)
	for b, bx := range boxes {
		if bx.hx >= bx.lx {
			out[b].centre = [2]float64{(bx.lx + bx.hx) / 2, (bx.lz + bx.hz) / 2}
			out[b].filled = true
		}
	}
	for _, p := range points {
		b := slice(p)
		if b < 0 || b >= n || !out[b].filled {
			continue
		}
		dx, dz := p[0]-out[b].centre[0], p[2]-out[b].centre[1]
		a := angleBin(dx, dz)
		out[b].radius[a] = math.Max(out[b].radius[a], math.Hypot(dx, dz))
	}
	// A direction no point fell in takes its neighbours' reach.
	for b := range out {
		if !out[b].filled {
			continue
		}
		for range 2 {
			next := out[b].radius
			for a := range sleeveAngles {
				if next[a] == 0 {
					next[a] = math.Max(out[b].radius[(a+1)%sleeveAngles], out[b].radius[(a+sleeveAngles-1)%sleeveAngles])
				}
			}
			out[b].radius = next
		}
	}
	return out
}

func angleBin(dx, dz float64) int {
	a := (math.Atan2(dz, dx) + math.Pi) / (2 * math.Pi) * sleeveAngles
	return int(a) % sleeveAngles
}

// sleeve fits the arm's muscles (the triangles at own) to the skin of the
// arm, in place.
func sleeve(muscles []triangle, own []int, skin []vec3) {
	if len(own) == 0 || len(skin) == 0 {
		return
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, p := range skin {
		lo, hi = math.Min(lo, p[1]), math.Max(hi, p[1])
	}
	n := int((hi-lo)/sleeveSlice) + 1
	var points []vec3
	for _, i := range own {
		points = append(points, muscles[i][0], muscles[i][1], muscles[i][2])
	}
	skinRings, muscleRings := rings(skin, lo, n), rings(points, lo, n)

	// Per slice: where the muscle's centre goes, and the stretch in each
	// direction. Both are smoothed up and down the arm so neighbouring slices
	// agree and the muscles keep their lines.
	type fit struct {
		from, to [2]float64
		stretch  [sleeveAngles]float64
		ok       bool
	}
	fits := make([]fit, n)
	for b := range fits {
		s, m := skinRings[b], muscleRings[b]
		if !s.filled || !m.filled {
			continue
		}
		fits[b] = fit{from: m.centre, to: s.centre, ok: true}
		for a := range sleeveAngles {
			f := 1.0
			if m.radius[a] > 0 && s.radius[a] > sleeveGap {
				f = (s.radius[a] - sleeveGap) / m.radius[a]
			}
			fits[b].stretch[a] = math.Min(sleeveMost, math.Max(sleeveLeast, f))
		}
	}
	smooth := make([]fit, n)
	for b := range fits {
		if !fits[b].ok {
			continue
		}
		var count float64
		out := fit{ok: true}
		for o := -3; o <= 3; o++ {
			if c := b + o; c >= 0 && c < n && fits[c].ok {
				count++
				for k := range 2 {
					out.from[k] += fits[c].from[k]
					out.to[k] += fits[c].to[k]
				}
				for a := range sleeveAngles {
					out.stretch[a] += (fits[c].stretch[(a+sleeveAngles-1)%sleeveAngles] + 2*fits[c].stretch[a] + fits[c].stretch[(a+1)%sleeveAngles]) / 4
				}
			}
		}
		for k := range 2 {
			out.from[k] /= count
			out.to[k] /= count
		}
		for a := range sleeveAngles {
			out.stretch[a] /= count
		}
		smooth[b] = out
	}

	move := func(p vec3) vec3 {
		b := int((p[1] - lo) / sleeveSlice)
		if b < 0 || b >= n || !smooth[b].ok {
			return p
		}
		f := smooth[b]
		dx, dz := p[0]-f.from[0], p[2]-f.from[1]
		// Between two directions the stretch is blended, so a muscle is not
		// faceted where it crosses from one to the next.
		at := (math.Atan2(dz, dx)+math.Pi)/(2*math.Pi)*sleeveAngles - 0.5
		a0 := int(math.Floor(at))
		t := at - float64(a0)
		a0 = ((a0 % sleeveAngles) + sleeveAngles) % sleeveAngles
		stretch := f.stretch[a0]*(1-t) + f.stretch[(a0+1)%sleeveAngles]*t
		q := vec3{f.to[0] + dx*stretch, p[1], f.to[1] + dz*stretch}
		// Eased in below the top of the sleeve: above it the arm joins the
		// shoulder, which stays where the pose put it.
		w := math.Min(1, math.Max(0, (hi-p[1])/sleeveBlend))
		w = w * w * (3 - 2*w)
		return p.add(q.sub(p).scale(w))
	}
	for _, i := range own {
		t := muscles[i]
		muscles[i] = triangle{move(t[0]), move(t[1]), move(t[2])}
	}
	var most float64
	for b := range smooth {
		for _, s := range smooth[b].stretch {
			most = math.Max(most, s)
		}
	}
	log.Printf("sleeve: arm fitted over %d slices, stretched up to ×%.2f", n, most)
}

// shoulderRise is how far above the armpit the sleeve runs on, over the cap
// of the shoulder.
const shoulderRise = 0.16

// withShoulder adds the skin of the shoulder to an arm's. Below the armpit
// the arm is its own piece of skin; above it the arm is whatever lies farther
// out than the armpit does.
func withShoulder(arm []vec3, skin mesh, sign float64) []vec3 {
	top := math.Inf(-1)
	for _, p := range arm {
		top = math.Max(top, p[1])
	}
	inner := math.Inf(1)
	for _, p := range arm {
		if p[1] > top-0.02 {
			inner = math.Min(inner, p[0]*sign)
		}
	}
	for i := range skin.tris {
		c := skin.triangle(i).centroid()
		if c[1] > top && c[1] < top+shoulderRise && c[0]*sign > inner {
			arm = append(arm, c)
		}
	}
	return arm
}

// armSkinOf is one side's arm skin, hand included, as points.
func armSkinOf(armSkin []skinSample, sign float64) []vec3 {
	var out []vec3
	for _, s := range armSkin {
		if s.at[0]*sign > 0 {
			out = append(out, s.at)
		}
	}
	return out
}
