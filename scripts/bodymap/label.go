package main

import (
	"math"
	"sort"
)

// baseRegion is skin that no muscle sits under: head, hands, feet, joints.
const baseRegion = "base"

// muscleIndex is a uniform grid over the atlas triangles, for "which muscle is
// nearest to this point of skin" without testing all of them.
type muscleIndex struct {
	cell  float64
	cells map[[3]int][]int
	tris  []triangle
	keys  []string
}

func newMuscleIndex(tris []triangle, keys []string, cell float64) *muscleIndex {
	idx := &muscleIndex{cell: cell, cells: map[[3]int][]int{}, tris: tris, keys: keys}
	for i, t := range tris {
		lo, hi := t[0], t[0]
		for _, v := range t[1:] {
			for k := range 3 {
				lo[k] = math.Min(lo[k], v[k])
				hi[k] = math.Max(hi[k], v[k])
			}
		}
		a, b := idx.cellOf(lo), idx.cellOf(hi)
		for x := a[0]; x <= b[0]; x++ {
			for y := a[1]; y <= b[1]; y++ {
				for z := a[2]; z <= b[2]; z++ {
					c := [3]int{x, y, z}
					idx.cells[c] = append(idx.cells[c], i)
				}
			}
		}
	}
	return idx
}

func (idx *muscleIndex) cellOf(p vec3) [3]int {
	return [3]int{int(math.Floor(p[0] / idx.cell)), int(math.Floor(p[1] / idx.cell)), int(math.Floor(p[2] / idx.cell))}
}

// nearest returns the key of the closest muscle surface within radius (at most
// one cell), or baseRegion.
func (idx *muscleIndex) nearest(p vec3, radius float64) string {
	if i, _, ok := idx.closest(p, radius); ok {
		return idx.keys[i]
	}
	return baseRegion
}

// closest is the nearest triangle within radius and the point on it.
func (idx *muscleIndex) closest(p vec3, radius float64) (int, vec3, bool) {
	best, bestIndex, bestPoint := radius, -1, vec3{}
	c := idx.cellOf(p)
	for x := c[0] - 1; x <= c[0]+1; x++ {
		for y := c[1] - 1; y <= c[1]+1; y++ {
			for z := c[2] - 1; z <= c[2]+1; z++ {
				for _, i := range idx.cells[[3]int{x, y, z}] {
					q := closestPointOnTriangle(p, idx.tris[i])
					if d := q.sub(p).length(); d < best {
						best, bestIndex, bestPoint = d, i, q
					}
				}
			}
		}
	}
	return bestIndex, bestPoint, bestIndex >= 0
}

// dropIslands hands every connected patch smaller than minTris to the label
// that surrounds it most, so a stray fleck of "biceps" on an elbow does not
// light up on its own.
func dropIslands(labels []string, adjacency [][]int, minTris int) {
	seen := make([]bool, len(labels))
	for start := range labels {
		if seen[start] {
			continue
		}
		patch := []int{start}
		seen[start] = true
		for i := 0; i < len(patch); i++ {
			for _, n := range adjacency[patch[i]] {
				if !seen[n] && labels[n] == labels[start] {
					seen[n] = true
					patch = append(patch, n)
				}
			}
		}
		if len(patch) >= minTris {
			continue
		}
		border := map[string]int{}
		for _, t := range patch {
			for _, n := range adjacency[t] {
				if labels[n] != labels[start] {
					border[labels[n]]++
				}
			}
		}
		if winner := mostCommon(border); winner != "" {
			for _, t := range patch {
				labels[t] = winner
			}
		}
	}
}

func mostCommon(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	best, bestCount := "", 0
	for _, k := range keys {
		if counts[k] > bestCount {
			best, bestCount = k, counts[k]
		}
	}
	return best
}

// labelVertices gives every skin vertex the key of the nearest muscle surface
// within radius, or baseRegion.
func labelVertices(skin mesh, idx *muscleIndex, radius float64) []string {
	out := make([]string, len(skin.verts))
	for i, v := range skin.verts {
		out[i] = idx.nearest(v, radius)
	}
	return out
}

// vertexNeighbours lists each vertex's one-ring.
func (m mesh) vertexNeighbours() [][]int {
	out := make([][]int, len(m.verts))
	for e := range m.edgeFaces() {
		out[e[0]] = append(out[e[0]], e[1])
		out[e[1]] = append(out[e[1]], e[0])
	}
	return out
}

// smoothVertices moves a vertex to the label most of its one-ring shares,
// which rounds off the notches a per-vertex nearest lookup leaves.
func smoothVertices(labels []string, neighbours [][]int, passes int) {
	for range passes {
		next := append([]string(nil), labels...)
		for i, ns := range neighbours {
			counts := map[string]int{}
			for _, n := range ns {
				counts[labels[n]]++
			}
			if winner := mostCommon(counts); winner != labels[i] && 2*counts[winner] > len(ns) {
				next[i] = winner
			}
		}
		copy(labels, next)
	}
}

// splitByLabels cuts every triangle whose corners disagree through its edge
// midpoints (and its centroid when all three differ), so a border between
// regions runs smoothly across triangles instead of stepping along their
// edges. Midpoints are shared between neighbouring triangles, which keeps the
// skin closed. Returns the cut mesh and each new triangle's label.
func splitByLabels(m mesh, labels []string) (mesh, []string) {
	out := mesh{verts: append([]vec3(nil), m.verts...)}
	var triLabels []string
	mid := map[edge]int{}
	midpoint := func(a, b int) int {
		e := edgeOf(a, b)
		if at, ok := mid[e]; ok {
			return at
		}
		mid[e] = len(out.verts)
		out.verts = append(out.verts, m.verts[a].add(m.verts[b]).scale(0.5))
		return mid[e]
	}
	emit := func(label string, tris ...[3]int) {
		for _, t := range tris {
			out.tris = append(out.tris, t)
			triLabels = append(triLabels, label)
		}
	}
	for _, t := range m.tris {
		// Rotate so that, when exactly two corners agree, they are a and b.
		a, b, c := t[0], t[1], t[2]
		switch {
		case labels[b] == labels[c] && labels[a] != labels[b]:
			a, b, c = b, c, a
		case labels[a] == labels[c] && labels[a] != labels[b]:
			a, b, c = c, a, b
		}
		la, lb, lc := labels[a], labels[b], labels[c]
		switch {
		case la == lb && lb == lc:
			emit(la, [3]int{a, b, c})
		case la == lb:
			mbc, mca := midpoint(b, c), midpoint(c, a)
			emit(la, [3]int{a, b, mbc}, [3]int{a, mbc, mca})
			emit(lc, [3]int{mca, mbc, c})
		default:
			mab, mbc, mca := midpoint(a, b), midpoint(b, c), midpoint(c, a)
			g := len(out.verts)
			out.verts = append(out.verts, m.verts[a].add(m.verts[b]).add(m.verts[c]).scale(1.0/3))
			emit(la, [3]int{a, mab, g}, [3]int{a, g, mca})
			emit(lb, [3]int{b, mbc, g}, [3]int{b, g, mab})
			emit(lc, [3]int{c, mca, g}, [3]int{c, g, mbc})
		}
	}
	return out, triLabels
}
