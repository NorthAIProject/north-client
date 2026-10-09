package main

import (
	"math"
	"sort"
)

// mesh is an indexed triangle mesh with shared vertices, which is what
// subdivision, smooth normals and triangle adjacency all need. The source skin
// arrives as triangle soup split along its UV seams.
type mesh struct {
	verts []vec3
	tris  [][3]int
}

// weld merges vertices closer than eps, joining the skin back across the seams
// its UV layout cut.
func weld(tris []triangle, eps float64) mesh {
	var m mesh
	index := map[[3]int64]int{}
	key := func(v vec3) [3]int64 {
		return [3]int64{int64(math.Round(v[0] / eps)), int64(math.Round(v[1] / eps)), int64(math.Round(v[2] / eps))}
	}
	for _, t := range tris {
		var face [3]int
		for i, v := range t {
			k := key(v)
			at, ok := index[k]
			if !ok {
				at = len(m.verts)
				index[k] = at
				m.verts = append(m.verts, v)
			}
			face[i] = at
		}
		if face[0] == face[1] || face[1] == face[2] || face[0] == face[2] {
			continue
		}
		m.tris = append(m.tris, face)
	}
	return m
}

type edge [2]int

func edgeOf(a, b int) edge {
	if a < b {
		return edge{a, b}
	}
	return edge{b, a}
}

// edgeFaces maps each edge to the triangles that use it.
func (m mesh) edgeFaces() map[edge][]int {
	out := make(map[edge][]int, len(m.tris)*3/2)
	for i, t := range m.tris {
		for k := range 3 {
			e := edgeOf(t[k], t[(k+1)%3])
			out[e] = append(out[e], i)
		}
	}
	return out
}

// loopSubdivide splits every triangle into four and smooths the result
// (Loop, 1987). Boundary and non-manifold edges take plain midpoints and their
// vertices stay put, which is all a closed character mesh with a few stray
// holes needs.
func loopSubdivide(m mesh) mesh {
	faces := m.edgeFaces()
	neighbours := make([]map[int]bool, len(m.verts))
	pinned := make([]bool, len(m.verts))
	for i := range neighbours {
		neighbours[i] = map[int]bool{}
	}
	for e, fs := range faces {
		neighbours[e[0]][e[1]] = true
		neighbours[e[1]][e[0]] = true
		if len(fs) != 2 {
			pinned[e[0]], pinned[e[1]] = true, true
		}
	}

	out := mesh{verts: make([]vec3, len(m.verts))}
	for i, v := range m.verts {
		n := len(neighbours[i])
		if pinned[i] || n < 3 {
			out.verts[i] = v
			continue
		}
		beta := 3.0 / (8 * float64(n))
		if n == 3 {
			beta = 3.0 / 16
		}
		// Summed in index order: map order would make the output differ
		// in the last bits from run to run, and churn the committed files.
		ring := make([]int, 0, n)
		for j := range neighbours[i] {
			ring = append(ring, j)
		}
		sort.Ints(ring)
		var sum vec3
		for _, j := range ring {
			sum = sum.add(m.verts[j])
		}
		out.verts[i] = v.scale(1 - float64(n)*beta).add(sum.scale(beta))
	}

	mid := map[edge]int{}
	midpoint := func(a, b int) int {
		e := edgeOf(a, b)
		if at, ok := mid[e]; ok {
			return at
		}
		va, vb := m.verts[e[0]], m.verts[e[1]]
		p := va.add(vb).scale(0.5)
		if fs := faces[e]; len(fs) == 2 {
			c, d := opposite(m.tris[fs[0]], e), opposite(m.tris[fs[1]], e)
			p = va.add(vb).scale(3.0 / 8).add(m.verts[c].add(m.verts[d]).scale(1.0 / 8))
		}
		mid[e] = len(out.verts)
		out.verts = append(out.verts, p)
		return mid[e]
	}

	for _, t := range m.tris {
		a, b, c := t[0], t[1], t[2]
		ab, bc, ca := midpoint(a, b), midpoint(b, c), midpoint(c, a)
		out.tris = append(out.tris, [3]int{a, ab, ca}, [3]int{ab, b, bc}, [3]int{ca, bc, c}, [3]int{ab, bc, ca})
	}
	return out
}

func opposite(t [3]int, e edge) int {
	for _, v := range t {
		if v != e[0] && v != e[1] {
			return v
		}
	}
	return t[0]
}

// normals are area-weighted vertex normals.
func (m mesh) normals() []vec3 {
	out := make([]vec3, len(m.verts))
	for _, t := range m.tris {
		n := m.verts[t[1]].sub(m.verts[t[0]]).cross(m.verts[t[2]].sub(m.verts[t[0]]))
		for _, v := range t {
			out[v] = out[v].add(n)
		}
	}
	for i := range out {
		out[i] = out[i].normalized()
	}
	return out
}

// adjacency lists each triangle's edge neighbours.
func (m mesh) adjacency() [][]int {
	out := make([][]int, len(m.tris))
	for _, fs := range m.edgeFaces() {
		for _, a := range fs {
			for _, b := range fs {
				if a != b {
					out[a] = append(out[a], b)
				}
			}
		}
	}
	return out
}

func (m mesh) triangle(i int) triangle {
	t := m.tris[i]
	return triangle{m.verts[t[0]], m.verts[t[1]], m.verts[t[2]]}
}

func (m mesh) bounds() (lo, hi vec3) {
	lo = vec3{math.Inf(1), math.Inf(1), math.Inf(1)}
	hi = vec3{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for _, v := range m.verts {
		for k := range 3 {
			lo[k] = math.Min(lo[k], v[k])
			hi[k] = math.Max(hi[k], v[k])
		}
	}
	return lo, hi
}
