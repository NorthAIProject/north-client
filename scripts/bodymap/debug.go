package main

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

// writeDebugSVG draws every skin triangle flat-shaded in its region's colour,
// front and back side by side, so a labelling change can be eyeballed without
// a browser. Painter's algorithm; good enough for a convex-ish body.
func writeDebugSVG(path string, skin mesh, labels []string) error {
	lo, hi := skin.bounds()
	scale := 500 / (hi[1] - lo[1])
	width := (hi[0] - lo[0]) * scale
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%.0f" height="520" style="background:#fff">`, 2*width+40)

	for view, sign := range []float64{1, -1} {
		order := make([]int, len(skin.tris))
		for i := range order {
			order[i] = i
		}
		sort.Slice(order, func(a, b int) bool {
			return sign*skin.triangle(order[a]).centroid()[2] < sign*skin.triangle(order[b]).centroid()[2]
		})
		offset := 10 + float64(view)*(width+20)
		sums := map[string][3]float64{}
		for _, i := range order {
			t := skin.triangle(i)
			n := t[1].sub(t[0]).cross(t[2].sub(t[0])).normalized()
			if sign*n[2] <= 0 {
				continue
			}
			light := 0.45 + 0.55*math.Max(0, n.dot(vec3{0.3 * sign, 0.5, sign}.normalized()))
			var points []string
			for _, v := range t {
				x := sign*(v[0]-(lo[0]+hi[0])/2)*scale + width/2 + offset
				y := (hi[1]-v[1])*scale + 10
				points = append(points, fmt.Sprintf("%.1f,%.1f", x, y))
			}
			fmt.Fprintf(&b, `<polygon points="%s" fill="%s"/>`, strings.Join(points, " "), debugColour(labels[i], light))
			c := t.centroid()
			if c[0] < (lo[0]+hi[0])/2 {
				continue // label one side only, or a limb pair averages to the midline
			}
			acc := sums[labels[i]]
			sums[labels[i]] = [3]float64{acc[0] + sign*(c[0]-(lo[0]+hi[0])/2)*scale + width/2 + offset, acc[1] + (hi[1]-c[1])*scale + 10, acc[2] + 1}
		}
		for label, acc := range sums {
			if label != baseRegion && acc[2] > 20 {
				fmt.Fprintf(&b, `<text x="%.0f" y="%.0f" font-size="9" font-family="sans-serif" text-anchor="middle" fill="#000" stroke="#fff" stroke-width="2" paint-order="stroke">%s</text>`, acc[0]/acc[2], acc[1]/acc[2], label)
			}
		}
	}
	b.WriteString(`</svg>`)
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// debugPalette gives neighbouring regions clearly different colours.
var debugPalette = map[string][3]int{
	"abs": {220, 60, 60}, "adductors": {240, 150, 40}, "biceps": {240, 200, 40},
	"calves": {140, 90, 200}, "chest": {60, 160, 230}, "delts": {230, 90, 170},
	"erectors": {250, 220, 120}, "forearms": {90, 200, 120}, "glutes": {60, 90, 200},
	"hamstrings": {200, 120, 60}, "lats": {40, 180, 180}, "neck": {150, 150, 60},
	"quads": {110, 70, 210}, "serratus": {250, 130, 120}, "traps": {120, 200, 60},
	"triceps": {60, 120, 160},
}

func debugColour(label string, light float64) string {
	c, ok := debugPalette[label]
	if !ok {
		c = [3]int{200, 200, 200}
	}
	return fmt.Sprintf("rgb(%d,%d,%d)", int(float64(c[0])*light), int(float64(c[1])*light), int(float64(c[2])*light))
}
