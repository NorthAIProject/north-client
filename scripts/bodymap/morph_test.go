package main

import (
	"math"
	"testing"
)

// tube is an open cylinder about a vertical axis at (x, z), from y0 to y1.
func tube(m *mesh, x, z, r, y0, y1 float64, rings, segments int) {
	base := len(m.verts)
	for i := 0; i <= rings; i++ {
		y := y0 + (y1-y0)*float64(i)/float64(rings)
		for j := range segments {
			a := 2 * math.Pi * float64(j) / float64(segments)
			m.verts = append(m.verts, vec3{x + r*math.Cos(a), y, z + r*math.Sin(a)})
		}
	}
	at := func(i, j int) int { return base + i*segments + j%segments }
	for i := range rings {
		for j := range segments {
			// Wound so normals face out.
			m.tris = append(m.tris, [3]int{at(i, j), at(i+1, j), at(i, j+1)}, [3]int{at(i, j+1), at(i+1, j), at(i+1, j+1)})
		}
	}
}

// toyBody is a torso one metre tall with two arms hanging beside it from 30%
// to 75% of its height, each its own piece of skin as on the real figure.
func toyBody() mesh {
	var m mesh
	tube(&m, 0, 0, 0.06, 0, 1, 200, 64)
	tube(&m, -0.2, 0, 0.03, 0.3, 0.75, 90, 24)
	tube(&m, 0.2, 0, 0.03, 0.3, 0.75, 90, 24)
	return m
}

func TestGirthOfACylinder(t *testing.T) {
	t.Parallel()
	var m mesh
	tube(&m, 0, 0, 0.1, 0, 1, 10, 128)
	g, ok := girthAt(m, 0.5)
	if want := 2 * math.Pi * 0.1; !ok || math.Abs(g-want)/want > 0.001 {
		t.Errorf("girth = %.4f, want %.4f", g, want)
	}
}

func TestConvexHullBridgesHollows(t *testing.T) {
	t.Parallel()
	// A unit square with a notch cut into one side: a tape goes straight
	// across the notch.
	notched := []vec2{{0, 0}, {1, 0}, {1, 1}, {0.6, 1}, {0.5, 0.5}, {0.4, 1}, {0, 1}}
	if p := perimeter(convexHull(notched)); math.Abs(p-4) > 1e-9 {
		t.Errorf("hull perimeter = %v, want 4", p)
	}
}

func TestShellMorphsReachTheirGirthsAndLeaveArmsAlone(t *testing.T) {
	t.Parallel()
	body := toyBody()
	targets, base, err := buildShellMorphs(body, body.normals())
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	s, err := analyseShell(body, body.normals())
	if err != nil {
		t.Fatalf("analyse: %v", err)
	}

	if from := 2 * math.Pi * 0.06; math.Abs(base.Waist.From-from) > 0.002 {
		t.Errorf("waist baseline = %.3f, want the tube's own %.3f", base.Waist.From, from)
	}

	centres := map[string]float64{"waist": s.waist, "hips": s.hips, "chest": s.chest}
	tos := map[string]float64{"waist": base.Waist.To, "hips": base.Hips.To, "chest": base.Chest.To}
	for _, tg := range targets {
		for i := range tg.deltas {
			if s.arm[i] && (tg.name != "overall" || body.verts[i][1] < s.hips) {
				t.Errorf("%s moves arm vertex %d at y %.2f", tg.name, i, body.verts[i][1])
				break
			}
		}
		centre, ok := centres[tg.name]
		if !ok {
			continue
		}
		moved := mesh{verts: append([]vec3(nil), body.verts...), tris: body.tris}
		for i, d := range tg.deltas {
			moved.verts[i] = moved.verts[i].add(d)
		}
		g, _ := girthAt(moved, centre)
		if want := tos[tg.name] * s.height; math.Abs(g-want)/want > 0.01 {
			t.Errorf("%s at full strength = %.3f of height, want %.2f", tg.name, g/s.height, tos[tg.name])
		}
	}

	// Nothing reaches above the cut, where the arms join the body.
	for _, tg := range targets[:3] {
		for i := range tg.deltas {
			if y := body.verts[i][1]; y > s.cut+1e-9 {
				t.Errorf("%s moves a vertex at %.3f, above the cut at %.3f", tg.name, y, s.cut)
				break
			}
		}
	}
}
