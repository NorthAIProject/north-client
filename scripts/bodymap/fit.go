package main

import (
	"log"
	"math"
	"sort"
)

// The atlas inside body.glb was fitted to the skin by matching total height —
// but the atlas has no skull, so it was stretched to reach the top of the
// head and every muscle sits about a tenth of the body too high (the glutes
// end up under the lower back). The glow renderer hid that; painting skin
// regions does not. So the atlas is refitted here, before labelling: scaled
// about the floor and nudged, to the pose where the most muscle sits just
// under the skin and the least pokes out through it.

// atlasFit maps an atlas point onto the skin.
type atlasFit struct {
	floor          vec3    // the point scaling is about: centre of the soles
	scale, scaleX  float64 // uniform scale, and an extra factor on width
	shiftY, shiftZ float64
}

func (f atlasFit) apply(p vec3) vec3 {
	d := p.sub(f.floor)
	return vec3{
		f.floor[0] + d[0]*f.scale*f.scaleX,
		f.floor[1] + d[1]*f.scale + f.shiftY,
		f.floor[2] + d[2]*f.scale + f.shiftZ,
	}
}

// fitAtlas searches scale, width and offset by coordinate descent, coarse to
// fine, against sampled atlas vertices scored on a signed-distance grid of the
// skin.
func fitAtlas(skin mesh, muscles []triangle) atlasFit {
	lo, hi := skin.bounds()
	fit := atlasFit{floor: vec3{(lo[0] + hi[0]) / 2, lo[1], (lo[2] + hi[2]) / 2}, scale: 1, scaleX: 1}
	field := newDistanceField(skin, fitVoxel)

	var samples []vec3
	for i := 0; i < len(muscles); i += 7 {
		samples = append(samples, muscles[i][0])
	}

	cost := func(f atlasFit) float64 {
		var sum float64
		for _, p := range samples {
			signed := field.at(f.apply(p))
			if signed > 0 {
				sum += 20 * signed * signed // through the skin
			} else if depth := -signed; depth > snugDepth {
				sum += (depth - snugDepth) * (depth - snugDepth) // sunk too deep
			}
		}
		return sum / float64(len(samples))
	}

	best := cost(fit)
	log.Printf("atlas fit: start cost %.6f", best)
	steps := []float64{0.04, 0.02, 0.01, 0.005, 0.0025}
	params := []func(*atlasFit) *float64{
		func(f *atlasFit) *float64 { return &f.scale },
		func(f *atlasFit) *float64 { return &f.scaleX },
		func(f *atlasFit) *float64 { return &f.shiftY },
		func(f *atlasFit) *float64 { return &f.shiftZ },
	}
	for _, step := range steps {
		for improved := true; improved; {
			improved = false
			for _, param := range params {
				for _, dir := range []float64{-1, 1} {
					trial := fit
					*param(&trial) += dir * step
					if c := cost(trial); c < best {
						best, fit, improved = c, trial, true
					}
				}
			}
		}
	}
	log.Printf("atlas fit: scale %.3f, width ×%.3f, shift y %+.3f z %+.3f, cost %.6f", fit.scale, fit.scaleX, fit.shiftY, fit.shiftZ, best)
	return fit
}

// distanceField is the skin's signed distance (positive outside) sampled on a
// voxel grid, so scoring a candidate fit is a lookup per point.
type distanceField struct {
	origin vec3
	voxel  float64
	n      [3]int
	values []float64
}

func newDistanceField(skin mesh, voxel float64) distanceField {
	lo, hi := skin.bounds()
	pad := vec3{fitPad, fitPad, fitPad}
	lo, hi = lo.sub(pad), hi.add(pad)
	f := distanceField{origin: lo, voxel: voxel}
	for k := range 3 {
		f.n[k] = int(math.Ceil((hi[k]-lo[k])/voxel)) + 1
	}
	tris := make([]triangle, len(skin.tris))
	for i := range skin.tris {
		tris[i] = skin.triangle(i)
	}
	inside := f.insideMask(tris)
	idx := newMuscleIndex(tris, make([]string, len(tris)), fitPad)
	f.values = make([]float64, f.n[0]*f.n[1]*f.n[2])
	for x := range f.n[0] {
		for y := range f.n[1] {
			for z := range f.n[2] {
				p := lo.add(vec3{float64(x), float64(y), float64(z)}.scale(voxel))
				distance := fitPad // farther than fitPad from the skin
				if _, c, ok := idx.closest(p, fitPad); ok {
					distance = p.sub(c).length()
				}
				at := (x*f.n[1]+y)*f.n[2] + z
				if inside[at] {
					distance = -distance
				}
				f.values[at] = distance
			}
		}
	}
	return f
}

// insideMask marks the voxels inside the skin by ray parity: for each (x, y)
// column, the z values where the column crosses the skin, sorted, alternate
// between entering and leaving.
func (f distanceField) insideMask(tris []triangle) []bool {
	crossings := make([][]float64, f.n[0]*f.n[1])
	for _, t := range tris {
		minX, maxX := math.Min(t[0][0], math.Min(t[1][0], t[2][0])), math.Max(t[0][0], math.Max(t[1][0], t[2][0]))
		minY, maxY := math.Min(t[0][1], math.Min(t[1][1], t[2][1])), math.Max(t[0][1], math.Max(t[1][1], t[2][1]))
		for x := int(math.Ceil((minX - f.origin[0]) / f.voxel)); float64(x) <= (maxX-f.origin[0])/f.voxel; x++ {
			for y := int(math.Ceil((minY - f.origin[1]) / f.voxel)); float64(y) <= (maxY-f.origin[1])/f.voxel; y++ {
				if x < 0 || y < 0 || x >= f.n[0] || y >= f.n[1] {
					continue
				}
				px, py := f.origin[0]+float64(x)*f.voxel, f.origin[1]+float64(y)*f.voxel
				if z, ok := zAt(t, px, py); ok {
					crossings[x*f.n[1]+y] = append(crossings[x*f.n[1]+y], z)
				}
			}
		}
	}
	inside := make([]bool, f.n[0]*f.n[1]*f.n[2])
	for column, zs := range crossings {
		sort.Float64s(zs)
		for k := 0; k+1 < len(zs); k += 2 {
			from := int(math.Ceil((zs[k] - f.origin[2]) / f.voxel))
			to := int(math.Floor((zs[k+1] - f.origin[2]) / f.voxel))
			for z := max(from, 0); z <= to && z < f.n[2]; z++ {
				inside[column*f.n[2]+z] = true
			}
		}
	}
	return inside
}

// zAt is where the vertical-in-z line through (x, y) meets triangle t, by
// barycentric coordinates in the xy plane.
func zAt(t triangle, x, y float64) (float64, bool) {
	x0, y0, x1, y1, x2, y2 := t[0][0], t[0][1], t[1][0], t[1][1], t[2][0], t[2][1]
	det := (y1-y2)*(x0-x2) + (x2-x1)*(y0-y2)
	if math.Abs(det) < 1e-12 {
		return 0, false
	}
	a := ((y1-y2)*(x-x2) + (x2-x1)*(y-y2)) / det
	b := ((y2-y0)*(x-x2) + (x0-x2)*(y-y2)) / det
	c := 1 - a - b
	if a < 0 || b < 0 || c < 0 {
		return 0, false
	}
	return a*t[0][2] + b*t[1][2] + c*t[2][2], true
}

// at is the nearest voxel's value; a point off the grid is outside.
func (f distanceField) at(p vec3) float64 {
	var c [3]int
	for k := range 3 {
		c[k] = int(math.Round((p[k] - f.origin[k]) / f.voxel))
		if c[k] < 0 || c[k] >= f.n[k] {
			return fitPad
		}
	}
	return f.values[(c[0]*f.n[1]+c[1])*f.n[2]+c[2]]
}

const (
	fitVoxel  = 0.008 // metres per distance-field cell
	fitPad    = 0.04  // how far the field looks from the skin; deeper counts as inside
	snugDepth = 0.03  // muscle this far under the skin or less is where it belongs
)
