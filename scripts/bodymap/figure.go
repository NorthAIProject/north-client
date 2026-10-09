package main

import (
	"fmt"
	"go/format"
	"math"
	"os"
	"sort"
	"strings"
)

// The flat figure is the no-WebGL fallback: the same regions seen from the
// front and the back, as SVG paths. Each view is rasterised into a label image
// (nearest triangle wins), each region's pixels are traced into closed
// outlines, and the outlines are simplified. Region borders therefore line up
// with the 3D figure's, which is the point of deriving both from one mesh.

const (
	figurePixel   = 0.003 // metres per raster pixel
	figureEpsilon = 0.75  // Douglas-Peucker tolerance, in pixels
	figurePad     = 4     // pixels of margin around the body
)

// labelImage is one view's raster: -1 for background, else an index into
// names.
type labelImage struct {
	w, h   int
	pixels []int
	names  []string
}

func rasterise(skin mesh, labels []string, back bool) labelImage {
	lo, hi := skin.bounds()
	names := []string{baseRegion}
	index := map[string]int{baseRegion: 0}
	for _, l := range labels {
		if _, ok := index[l]; !ok {
			index[l] = len(names)
			names = append(names, l)
		}
	}
	img := labelImage{
		w:     int(math.Ceil((hi[0]-lo[0])/figurePixel)) + 2*figurePad,
		h:     int(math.Ceil((hi[1]-lo[1])/figurePixel)) + 2*figurePad,
		names: names,
	}
	img.pixels = make([]int, img.w*img.h)
	depth := make([]float64, img.w*img.h)
	for i := range img.pixels {
		img.pixels[i] = -1
		depth[i] = math.Inf(-1)
	}
	project := func(v vec3) (float64, float64, float64) {
		x := v[0] - lo[0]
		if back {
			x = hi[0] - v[0]
		}
		z := v[2]
		if back {
			z = -z
		}
		return x/figurePixel + figurePad, (hi[1]-v[1])/figurePixel + figurePad, z
	}
	for ti, t := range skin.tris {
		var px, py, pz [3]float64
		for k, v := range t {
			px[k], py[k], pz[k] = project(skin.verts[v])
		}
		minX, maxX := int(math.Floor(min(px[0], px[1], px[2]))), int(math.Ceil(max(px[0], px[1], px[2])))
		minY, maxY := int(math.Floor(min(py[0], py[1], py[2]))), int(math.Ceil(max(py[0], py[1], py[2])))
		det := (py[1]-py[2])*(px[0]-px[2]) + (px[2]-px[1])*(py[0]-py[2])
		if math.Abs(det) < 1e-12 {
			continue
		}
		for y := max(minY, 0); y <= maxY && y < img.h; y++ {
			for x := max(minX, 0); x <= maxX && x < img.w; x++ {
				cx, cy := float64(x)+0.5, float64(y)+0.5
				a := ((py[1]-py[2])*(cx-px[2]) + (px[2]-px[1])*(cy-py[2])) / det
				b := ((py[2]-py[0])*(cx-px[2]) + (px[0]-px[2])*(cy-py[2])) / det
				c := 1 - a - b
				if a < 0 || b < 0 || c < 0 {
					continue
				}
				z := a*pz[0] + b*pz[1] + c*pz[2]
				at := y*img.w + x
				if z > depth[at] {
					depth[at] = z
					img.pixels[at] = index[labels[ti]]
				}
			}
		}
	}
	return img
}

type point struct{ x, y float64 }

// outlines traces the borders of the pixels where in(label) holds into closed
// loops, by following pixel edges with the region kept on one side. Holes come
// out wound the other way, so the paths fill correctly with evenodd.
func (img labelImage) outlines(in func(label int) bool) [][]point {
	inside := func(x, y int) bool {
		return x >= 0 && y >= 0 && x < img.w && y < img.h && in(img.pixels[y*img.w+x])
	}
	type vertex struct{ x, y int }
	next := map[vertex][]vertex{}
	for y := range img.h {
		for x := range img.w {
			if !inside(x, y) {
				continue
			}
			if !inside(x, y-1) {
				next[vertex{x, y}] = append(next[vertex{x, y}], vertex{x + 1, y})
			}
			if !inside(x+1, y) {
				next[vertex{x + 1, y}] = append(next[vertex{x + 1, y}], vertex{x + 1, y + 1})
			}
			if !inside(x, y+1) {
				next[vertex{x + 1, y + 1}] = append(next[vertex{x + 1, y + 1}], vertex{x, y + 1})
			}
			if !inside(x-1, y) {
				next[vertex{x, y + 1}] = append(next[vertex{x, y + 1}], vertex{x, y})
			}
		}
	}
	starts := make([]vertex, 0, len(next))
	for v := range next {
		starts = append(starts, v)
	}
	sort.Slice(starts, func(i, j int) bool {
		if starts[i].y != starts[j].y {
			return starts[i].y < starts[j].y
		}
		return starts[i].x < starts[j].x
	})

	var loops [][]point
	for _, start := range starts {
		for len(next[start]) > 0 {
			var loop []point
			prev, at := vertex{}, start
			first := true
			for {
				outs := next[at]
				if len(outs) == 0 {
					break
				}
				// At a saddle (two regions touching corner to corner) always
				// take the right turn, so loops never cross.
				pick := 0
				if len(outs) > 1 && !first {
					dx, dy := at.x-prev.x, at.y-prev.y
					for i, o := range outs {
						if o.x-at.x == -dy && o.y-at.y == dx {
							pick = i
						}
					}
				}
				to := outs[pick]
				next[at] = append(outs[:pick:pick], outs[pick+1:]...)
				loop = append(loop, point{float64(at.x), float64(at.y)})
				prev, at, first = at, to, false
				if at == start {
					break
				}
			}
			if len(loop) >= 4 {
				loops = append(loops, loop)
			}
		}
	}
	return loops
}

// simplify is Douglas-Peucker on a closed loop, split at its two farthest
// apart points.
func simplify(loop []point, eps float64) []point {
	far, best := 0, 0.0
	for i, p := range loop {
		if d := math.Hypot(p.x-loop[0].x, p.y-loop[0].y); d > best {
			far, best = i, d
		}
	}
	a := dp(loop[:far+1], eps)
	b := dp(append(append([]point{}, loop[far:]...), loop[0]), eps)
	return append(a[:len(a)-1], b[:len(b)-1]...)
}

func dp(pts []point, eps float64) []point {
	if len(pts) < 3 {
		return pts
	}
	a, b := pts[0], pts[len(pts)-1]
	far, best := 0, -1.0
	for i := 1; i < len(pts)-1; i++ {
		if d := segmentDistance(pts[i], a, b); d > best {
			far, best = i, d
		}
	}
	if best <= eps {
		return []point{a, b}
	}
	left := dp(pts[:far+1], eps)
	right := dp(pts[far:], eps)
	return append(left[:len(left)-1], right...)
}

func segmentDistance(p, a, b point) float64 {
	dx, dy := b.x-a.x, b.y-a.y
	if dx == 0 && dy == 0 {
		return math.Hypot(p.x-a.x, p.y-a.y)
	}
	t := math.Max(0, math.Min(1, ((p.x-a.x)*dx+(p.y-a.y)*dy)/(dx*dx+dy*dy)))
	return math.Hypot(p.x-a.x-t*dx, p.y-a.y-t*dy)
}

func pathData(loops [][]point) string {
	var b strings.Builder
	for _, loop := range loops {
		loop = simplify(loop, figureEpsilon)
		if len(loop) < 3 {
			continue
		}
		for i, p := range loop {
			if i == 0 {
				fmt.Fprintf(&b, "M%g %g", p.x, p.y)
			} else {
				fmt.Fprintf(&b, "L%g %g", p.x, p.y)
			}
		}
		b.WriteString("Z")
	}
	return b.String()
}

// figureSVG is a standalone preview of both views, for -debug-svg.
func figureSVG(skin mesh, labels []string) string {
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="600" height="560" style="background:#fff">`)
	for i, back := range []bool{false, true} {
		img := rasterise(skin, labels, back)
		fmt.Fprintf(&b, `<g transform="translate(%d 0) scale(%.3f)">`, i*300, 540/float64(img.h))
		fmt.Fprintf(&b, `<path fill="#9aa6b5" fill-rule="evenodd" d="%s"/>`, pathData(img.outlines(func(l int) bool { return l >= 0 })))
		for k, name := range img.names {
			if name == baseRegion {
				continue
			}
			c := debugPalette[name]
			fmt.Fprintf(&b, `<path fill="rgb(%d,%d,%d)" fill-rule="evenodd" stroke="#fff" stroke-width="0.6" d="%s"/>`, c[0], c[1], c[2], pathData(img.outlines(func(l int) bool { return l == k })))
		}
		b.WriteString(`</g>`)
	}
	b.WriteString(`</svg>`)
	return b.String()
}

// writeFigure writes the generated Go file the templ fallback renders: per
// view, the whole silhouette first and then one path per region.
func writeFigure(path string, skin mesh, labels []string) error {
	var b strings.Builder
	b.WriteString("// Code generated by scripts/bodymap. DO NOT EDIT.\n\npackage muscleviewer\n\n")
	for _, view := range []struct {
		name string
		back bool
	}{{"figureFront", false}, {"figureBack", true}} {
		img := rasterise(skin, labels, view.back)
		fmt.Fprintf(&b, "var %s = figureView{\n\tViewBox: %q,\n", view.name, fmt.Sprintf("0 0 %d %d", img.w, img.h))
		fmt.Fprintf(&b, "\tSilhouette: %q,\n\tRegions: []figureRegion{\n", pathData(img.outlines(func(l int) bool { return l >= 0 })))
		order := make([]int, 0, len(img.names))
		for i, name := range img.names {
			if name != baseRegion {
				order = append(order, i)
			}
		}
		sort.Slice(order, func(i, j int) bool { return img.names[order[i]] < img.names[order[j]] })
		for _, i := range order {
			d := pathData(img.outlines(func(l int) bool { return l == i }))
			if d == "" {
				continue
			}
			fmt.Fprintf(&b, "\t\t{Region: %q, D: %q},\n", img.names[i], d)
		}
		b.WriteString("\t},\n}\n\n")
	}
	src, err := format.Source([]byte(b.String()))
	if err != nil {
		return fmt.Errorf("format generated figure: %w", err)
	}
	return os.WriteFile(path, src, 0o644)
}
