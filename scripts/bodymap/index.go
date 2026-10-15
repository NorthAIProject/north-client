package main

import "math"

// triangleIndex is a uniform grid over a triangle soup, for "which triangle is
// nearest to this point" without testing all of them. The fit scores atlas
// points against the skin with it, and pushInside finds where to tuck a
// stray vertex.
type triangleIndex struct {
	cell  float64
	cells map[[3]int][]int
	tris  []triangle
}

func newTriangleIndex(tris []triangle, cell float64) *triangleIndex {
	idx := &triangleIndex{cell: cell, cells: map[[3]int][]int{}, tris: tris}
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

func (idx *triangleIndex) cellOf(p vec3) [3]int {
	return [3]int{int(math.Floor(p[0] / idx.cell)), int(math.Floor(p[1] / idx.cell)), int(math.Floor(p[2] / idx.cell))}
}

// closest is the nearest triangle within radius (at most one cell) and the
// point on it.
func (idx *triangleIndex) closest(p vec3, radius float64) (int, vec3, bool) {
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

// skinIndex indexes a mesh's triangles.
func skinIndex(skin mesh) *triangleIndex {
	tris := make([]triangle, len(skin.tris))
	for i := range skin.tris {
		tris[i] = skin.triangle(i)
	}
	return newTriangleIndex(tris, fitPad)
}
