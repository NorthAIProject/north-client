package main

import (
	"math"
	"testing"
)

// Every vertex survives the int16 grid to within half a step, and the step is
// set by the figure's longest side.
func TestQuantizerRoundTrip(t *testing.T) {
	regions := []region{{verts: []vec3{{-0.3, 0, -0.15}, {0.3, 1.5, 0.15}, {0.123456, 0.654321, -0.012345}}}}
	q := newQuantizer(regions)
	if want := 1.5 / 65534; math.Abs(q.step-want) > 1e-12 {
		t.Errorf("step = %g, want %g", q.step, want)
	}
	for _, v := range regions[0].verts {
		back := q.decode(q.encode(v))
		for k := range 3 {
			if math.Abs(back[k]-v[k]) > q.step/2+1e-12 {
				t.Errorf("vertex %v came back as %v", v, back)
			}
		}
	}
	// The root node transform must agree with decode.
	tr := q.translation()
	if got := q.decode([3]int16{0, 0, 0}); got[0] != tr[0] || got[1] != tr[1] || got[2] != tr[2] {
		t.Errorf("translation %v != decode(0) %v", tr, got)
	}
}
