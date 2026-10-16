package main

import (
	"fmt"
	"math"
	"os"

	"github.com/NorthAIProject/north-client/internal/bodymap"
)

// morphTarget is one shape key on the shell: a displacement per vertex, in
// metres, zero for every vertex not listed.
type morphTarget struct {
	name   string
	deltas map[int]vec3
}

// band is one circumference a morph target widens: where on the body, how far
// up and down it reaches, which way it leans, and the girth it reaches at
// full strength, as circumference over standing height.
type band struct {
	name string

	// half is how far above and below the landmark the target reaches, as a
	// fraction of height. The weight eases from 1 at the landmark to 0 there.
	// Wide enough that neighbouring bands overlap: narrow ones stack into
	// separate rolls instead of one outline.
	half float64

	// front and back lean the push: a belly grows forward, a seat backward.
	// 0.6 pushes the front of the body 60% further than its sides.
	front, back float64

	// to is the girth at full strength over height. Past it the server
	// clamps the weight at 1.
	to float64
}

var (
	waistBand = band{name: "waist", half: 0.10, front: 0.45, to: 0.75}
	hipsBand  = band{name: "hips", half: 0.09, back: 0.2, to: 0.80}
	// The chest widens evenly: a forward lean there reads as a bust.
	chestBand = band{name: "chest", half: 0.08, to: 0.82}
)

// overallMetres is how far the overall target moves the body along its
// normal at full strength. The server caps overall at 0.4, so the most the
// figure ever thickens is 6 mm: enough that a heavier body does not look
// hollow where nothing was measured, never enough to bloat it. Hands and the
// head are left alone; a few millimetres on a finger reads as a mitten.
const overallMetres = 0.015

// sliceBins is how finely the shell is sliced to find landmarks and to
// assign vertices to the loop they belong to: about 5 mm on this figure.
const sliceBins = 300

// shellShape is everything the morph targets are built from: the shell's own
// slices, which loop each vertex belongs to, and its landmarks.
type shellShape struct {
	skin    mesh
	normals []vec3
	height  float64

	slices [][]loopInfo // per bin, bottom to top
	owner  []loopRef    // per vertex: its bin and loop, or -1 for an arm

	waist, hips, chest, armpit float64 // landmark heights, metres

	// cut is where the arms join the body. No band reaches above it, so
	// nothing pushes the shoulders or the tops of the arms.
	cut float64

	// arm marks the vertices of the arms and hands below the armpit.
	arm []bool
}

type loopInfo struct {
	points []vec2
	centre vec2
	arm    bool
}

type loopRef struct{ bin, loop int }

// armOffset is how far from the body's axis a slice loop's centre must be to
// count as an arm or a hand, as a fraction of height. Legs sit at about 0.06,
// hands and forearms at 0.13 and beyond.
const armOffset = 0.1

// buildShellMorphs makes the four shape keys and measures the shell's own
// baseline girths. The skin must already stand on the origin.
func buildShellMorphs(skin mesh, normals []vec3) ([]morphTarget, bodymap.Baseline, error) {
	s, err := analyseShell(skin, normals)
	if err != nil {
		return nil, bodymap.Baseline{}, err
	}
	var targets []morphTarget
	var base bodymap.Baseline
	for _, b := range []struct {
		band   band
		centre float64
		girth  *bodymap.Girth
	}{
		{waistBand, s.waist, &base.Waist},
		{hipsBand, s.hips, &base.Hips},
		{chestBand, s.chest, &base.Chest},
	} {
		target, from, err := s.solveBand(b.band, b.centre)
		if err != nil {
			return nil, bodymap.Baseline{}, err
		}
		*b.girth = bodymap.Girth{From: round3(from / s.height), To: b.band.to}
		targets = append(targets, target)
	}
	targets = append(targets, s.overall())
	return targets, base, nil
}

func analyseShell(skin mesh, normals []vec3) (*shellShape, error) {
	lo, hi := skin.bounds()
	s := &shellShape{skin: skin, normals: normals, height: hi[1] - lo[1]}
	armX := armOffset * s.height

	s.slices = make([][]loopInfo, sliceBins)
	for b := range sliceBins {
		y := s.binY(b)
		for _, l := range sliceLoops(skin, y) {
			c := centre(l)
			s.slices[b] = append(s.slices[b], loopInfo{points: l, centre: c, arm: math.Abs(c[0]) > armX})
		}
	}

	// Landmarks, from the girth of the central loop at each height.
	girth := make([]float64, sliceBins)
	single := make([]bool, sliceBins) // the torso is the only loop: arms joined
	for b, loops := range s.slices {
		arms := 0
		for _, l := range loops {
			if l.arm {
				arms++
				continue
			}
			if math.Abs(l.centre[0]) < 0.03*s.height {
				girth[b] = perimeter(convexHull(l.points))
			}
		}
		single[b] = arms == 0 && len(loops) == 1
	}
	pick := func(from, to float64, better func(a, b float64) bool) (float64, bool) {
		best, found := 0.0, false
		var bestBin int
		for b := s.bin(from * s.height); b <= s.bin(to*s.height); b++ {
			if girth[b] == 0 {
				continue
			}
			if !found || better(girth[b], best) {
				best, bestBin, found = girth[b], b, true
			}
		}
		return s.binY(bestBin), found
	}
	var ok bool
	if s.hips, ok = pick(0.46, 0.56, func(a, b float64) bool { return a > b }); !ok {
		return nil, fmt.Errorf("morphs: no hip girth found between 46%% and 56%% of height")
	}
	if s.waist, ok = pick(0.56, 0.70, func(a, b float64) bool { return a < b }); !ok {
		return nil, fmt.Errorf("morphs: no waist found between 56%% and 70%% of height")
	}
	for b := s.bin(0.70 * s.height); b < sliceBins; b++ {
		if single[b] {
			s.armpit = s.binY(b)
			break
		}
	}
	if s.armpit == 0 {
		return nil, fmt.Errorf("morphs: no armpit found above 70%% of height")
	}
	// Below the armpit each arm is its own piece of skin, so the arms are the
	// connected pieces that are not the body's. Measured by topology rather
	// than by distance: the inside of a forearm hangs closer to the waist's
	// skin than to its own arm's slice.
	s.cut = s.armpit - 0.02*s.height
	s.arm = armVertices(skin, s.cut, armX)

	// The chest is taken a little under the cut, about nipple height; its
	// band reaches its full depth below and stops at the cut above.
	s.chest = s.cut - 0.035*s.height

	// Every other vertex belongs to the nearest body loop on its slice: the
	// torso, or below the crotch its own leg, which is pushed about its own
	// axis.
	s.owner = make([]loopRef, len(skin.verts))
	for i, v := range skin.verts {
		if s.arm[i] {
			s.owner[i] = loopRef{bin: -1}
			continue
		}
		b := s.bin(v[1])
		best, bestDist := -1, math.Inf(1)
		for j, l := range s.slices[b] {
			if l.arm {
				continue
			}
			for _, p := range l.points {
				if d := math.Hypot(p[0]-v[0], p[1]-v[2]); d < bestDist {
					best, bestDist = j, d
				}
			}
		}
		if best < 0 {
			s.owner[i] = loopRef{bin: -1}
			continue
		}
		s.owner[i] = loopRef{bin: b, loop: best}
	}
	return s, nil
}

func (s *shellShape) binY(b int) float64 { return (float64(b) + 0.5) / sliceBins * s.height }

func (s *shellShape) bin(y float64) int {
	b := int(y / s.height * sliceBins)
	return max(0, min(sliceBins-1, b))
}

// weight is how strongly the band at centre moves a vertex at height y. It
// never reaches above the cut.
func (s *shellShape) weight(bd band, centre, y float64) float64 {
	reach := bd.half * s.height
	if y > centre {
		reach = math.Min(reach, s.cut-centre)
	}
	if reach <= 0 {
		return 0
	}
	t := 1 - math.Abs(y-centre)/reach
	if t <= 0 {
		return 0
	}
	return t * t * (3 - 2*t) // smoothstep: no crease where the band ends
}

// deltas is the band's displacement at strength k metres: each vertex moves
// straight out from its loop's centre, horizontally, by k times its weight
// and lean. The same distance for every vertex at a height, not a scale: a
// scale would grow the muscle relief with the body, and a large waist would
// read as a stack of rolls.
func (s *shellShape) deltas(bd band, centre, k float64) map[int]vec3 {
	out := map[int]vec3{}
	for i, v := range s.skin.verts {
		ref := s.owner[i]
		if ref.bin < 0 {
			continue
		}
		w := s.weight(bd, centre, v[1])
		if w == 0 {
			continue
		}
		c := s.slices[ref.bin][ref.loop].centre
		dx, dz := v[0]-c[0], v[2]-c[1]
		r := math.Hypot(dx, dz)
		if r == 0 {
			continue
		}
		lean := 1 + bd.front*math.Max(0, dz/r) + bd.back*math.Max(0, -dz/r)
		f := k * w * lean / r
		out[i] = vec3{dx * f, 0, dz * f}
	}
	return out
}

// solveBand finds the strength at which the band's girth reaches its target,
// by bisection on the measured girth: the lean makes the push uneven, so the
// answer is measured rather than worked out. It returns the target and the
// shell's girth there as modelled.
func (s *shellShape) solveBand(bd band, centre float64) (morphTarget, float64, error) {
	from, ok := girthAt(s.skin, centre)
	if !ok {
		return morphTarget{}, 0, fmt.Errorf("morphs: no %s girth at %.3f m", bd.name, centre)
	}
	want := bd.to * s.height
	if want <= from {
		return morphTarget{}, 0, fmt.Errorf("morphs: %s target %.3f is not wider than the shell's %.3f", bd.name, want, from/s.height)
	}
	measure := func(k float64) float64 {
		moved := mesh{verts: append([]vec3(nil), s.skin.verts...), tris: s.skin.tris}
		for i, d := range s.deltas(bd, centre, k) {
			moved.verts[i] = moved.verts[i].add(d)
		}
		g, _ := girthAt(moved, centre)
		return g
	}
	lo, hi := 0.0, 0.5 // metres
	if measure(hi) < want {
		return morphTarget{}, 0, fmt.Errorf("morphs: %s cannot reach %.2f of height", bd.name, bd.to)
	}
	for range 30 {
		mid := (lo + hi) / 2
		if measure(mid) < want {
			lo = mid
		} else {
			hi = mid
		}
	}
	return morphTarget{name: bd.name, deltas: s.deltas(bd, centre, hi)}, from, nil
}

// overall moves the body out along its normal: the torso, the legs and the
// arms down to the wrist, easing off up the neck so the head keeps its size.
func (s *shellShape) overall() morphTarget {
	out := make(map[int]vec3, len(s.skin.verts))
	neck, crown := 0.82*s.height, 0.86*s.height
	for i, n := range s.normals {
		v := s.skin.verts[i]
		if s.arm[i] && v[1] < s.hips { // the hands hang below the hips
			continue
		}
		w := 1.0
		if v[1] > neck {
			w = math.Max(0, (crown-v[1])/(crown-neck))
		}
		if w > 0 {
			out[i] = n.scale(overallMetres * w)
		}
	}
	return morphTarget{name: "overall", deltas: out}
}

// armVertices marks the skin of the arms below cut: the connected pieces of
// the skin under that height whose centre is off the body's axis.
func armVertices(skin mesh, cut, armX float64) []bool {
	parent := make([]int, len(skin.verts))
	for i := range parent {
		parent[i] = i
	}
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	below := func(i int) bool { return skin.verts[i][1] < cut }
	for _, t := range skin.tris {
		for k := range 3 {
			a, b := t[k], t[(k+1)%3]
			if below(a) && below(b) {
				parent[find(a)] = find(b)
			}
		}
	}
	sumX := map[int]float64{}
	count := map[int]int{}
	for i, v := range skin.verts {
		if below(i) {
			r := find(i)
			sumX[r] += v[0]
			count[r]++
		}
	}
	arm := make([]bool, len(skin.verts))
	for i := range skin.verts {
		if below(i) {
			r := find(i)
			arm[i] = math.Abs(sumX[r]/float64(count[r])) > armX
		}
	}
	return arm
}

func round3(v float64) float64 { return math.Round(v*1000) / 1000 }

// writeBaseline writes the shell's girths as Go, the way figure_gen.go is
// written, so the server and the mesh are rebuilt from one run.
func writeBaseline(path string, b bodymap.Baseline) error {
	src := fmt.Sprintf(`// Code generated by scripts/bodymap; DO NOT EDIT.

package bodymap

// ShellBaseline is the figure's own girths over height, measured off the
// shell: From as modelled, To with the shape key at full strength.
var ShellBaseline = Baseline{
	Waist: Girth{From: %.3f, To: %.2f},
	Hips:  Girth{From: %.3f, To: %.2f},
	Chest: Girth{From: %.3f, To: %.2f},
}
`, b.Waist.From, b.Waist.To, b.Hips.From, b.Hips.To, b.Chest.From, b.Chest.To)
	return os.WriteFile(path, []byte(src), 0o644)
}
