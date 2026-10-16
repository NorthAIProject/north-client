package main

import (
	"math"
	"sort"
)

// vec2 is a point on a horizontal slice: x and z.
type vec2 [2]float64

// sliceLoops cuts the mesh with the horizontal plane at height y and chains
// the cut into closed loops. At torso height the arms hang beside the body,
// so a slice there is several loops: the torso and one per arm.
func sliceLoops(m mesh, y float64) [][]vec2 {
	type key [2]int64
	snap := func(p vec2) key { return key{int64(math.Round(p[0] * 1e7)), int64(math.Round(p[1] * 1e7))} }

	// Each crossed triangle gives one segment; its ends lie on edges, so
	// neighbouring triangles share them exactly once snapped.
	next := map[key][]key{}
	point := map[key]vec2{}
	for _, t := range m.tris {
		var ends []vec2
		for k := range 3 {
			a, b := m.verts[t[k]], m.verts[t[(k+1)%3]]
			if (a[1] < y) == (b[1] < y) {
				continue
			}
			s := (y - a[1]) / (b[1] - a[1])
			ends = append(ends, vec2{a[0] + s*(b[0]-a[0]), a[2] + s*(b[2]-a[2])})
		}
		if len(ends) != 2 {
			continue
		}
		ka, kb := snap(ends[0]), snap(ends[1])
		if ka == kb {
			continue
		}
		point[ka], point[kb] = ends[0], ends[1]
		next[ka] = append(next[ka], kb)
		next[kb] = append(next[kb], ka)
	}

	seen := map[key]bool{}
	starts := make([]key, 0, len(point))
	for k := range point {
		starts = append(starts, k)
	}
	sort.Slice(starts, func(i, j int) bool {
		return starts[i][0] < starts[j][0] || (starts[i][0] == starts[j][0] && starts[i][1] < starts[j][1])
	})

	var loops [][]vec2
	for _, start := range starts {
		if seen[start] {
			continue
		}
		var loop []vec2
		at := start
		for !seen[at] {
			seen[at] = true
			loop = append(loop, point[at])
			moved := false
			for _, n := range next[at] {
				if !seen[n] {
					at, moved = n, true
					break
				}
			}
			if !moved {
				break
			}
		}
		if len(loop) >= 3 {
			loops = append(loops, loop)
		}
	}
	return loops
}

// torsoLoop is the loop around the body's axis: the one whose centre is
// nearest x = 0, among loops big enough to be a body rather than a finger.
func torsoLoop(loops [][]vec2) ([]vec2, bool) {
	best, bestDist := -1, math.Inf(1)
	for i, l := range loops {
		if math.Abs(area(l)) < 1e-3 { // 10 cm²: a finger, a seam artefact
			continue
		}
		c := centre(l)
		if d := math.Abs(c[0]); d < bestDist {
			best, bestDist = i, d
		}
	}
	if best < 0 {
		return nil, false
	}
	return loops[best], true
}

// girthAt is the body's circumference at height y as a tape takes it: around
// the torso loop's convex hull, bridging the hollows a tape does not follow.
func girthAt(m mesh, y float64) (float64, bool) {
	loop, ok := torsoLoop(sliceLoops(m, y))
	if !ok {
		return 0, false
	}
	return perimeter(convexHull(loop)), true
}

func centre(l []vec2) vec2 {
	var c vec2
	for _, p := range l {
		c[0] += p[0]
		c[1] += p[1]
	}
	n := float64(len(l))
	return vec2{c[0] / n, c[1] / n}
}

// area is the shoelace area of a closed loop, signed by winding.
func area(l []vec2) float64 {
	var s float64
	for i := range l {
		a, b := l[i], l[(i+1)%len(l)]
		s += a[0]*b[1] - b[0]*a[1]
	}
	return s / 2
}

func perimeter(l []vec2) float64 {
	var s float64
	for i := range l {
		a, b := l[i], l[(i+1)%len(l)]
		s += math.Hypot(b[0]-a[0], b[1]-a[1])
	}
	return s
}

// convexHull is Andrew's monotone chain.
func convexHull(pts []vec2) []vec2 {
	p := append([]vec2(nil), pts...)
	sort.Slice(p, func(i, j int) bool { return p[i][0] < p[j][0] || (p[i][0] == p[j][0] && p[i][1] < p[j][1]) })
	cross := func(o, a, b vec2) float64 { return (a[0]-o[0])*(b[1]-o[1]) - (a[1]-o[1])*(b[0]-o[0]) }
	var hull []vec2
	for pass := 0; pass < 2; pass++ {
		start := len(hull)
		for _, q := range p {
			for len(hull) >= start+2 && cross(hull[len(hull)-2], hull[len(hull)-1], q) <= 0 {
				hull = hull[:len(hull)-1]
			}
			hull = append(hull, q)
		}
		hull = hull[:len(hull)-1]
		for i, j := 0, len(p)-1; i < j; i, j = i+1, j-1 {
			p[i], p[j] = p[j], p[i]
		}
	}
	return hull
}
