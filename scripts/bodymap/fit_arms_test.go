package main

import (
	"math"
	"testing"
)

func TestArmPose(t *testing.T) {
	pivot := vec3{0.2, 1.4, 0}
	p := pivot.add(vec3{0, -0.3, 0}) // a point down the arm
	if got := (armPose{pivot: pivot}).apply(p); got != p {
		t.Errorf("zero pose moved %v to %v", p, got)
	}
	// Abducting a quarter turn swings the hanging arm out sideways.
	got := (armPose{pivot: pivot, abduct: -math.Pi / 2}).apply(p)
	want := pivot.add(vec3{-0.3, 0, 0})
	if got.sub(want).length() > 1e-9 {
		t.Errorf("abduct: got %v, want %v", got, want)
	}
	got = (armPose{pivot: pivot, shift: vec3{0, -0.02, 0}}).apply(p)
	if want := p.add(vec3{0, -0.02, 0}); got != want {
		t.Errorf("shift: got %v, want %v", got, want)
	}
}

func TestArmPoseInvert(t *testing.T) {
	pose := armPose{pivot: vec3{0.2, 1.4, 0}, abduct: 0.1, flex: -0.07, shift: vec3{0.01, -0.02, 0.03}}
	p := vec3{0.25, 1.1, -0.04}
	if got := pose.invert(pose.apply(p)); got.sub(p).length() > 1e-9 {
		t.Errorf("invert(apply(p)) = %v, want %v", got, p)
	}
}

// A tube of muscle half as thick as the tube of skin round it, and off to one
// side of it, comes out filling the skin.
func TestSleeveFillsTheSkin(t *testing.T) {
	tube := func(cx, cz, r float64) []vec3 {
		var out []vec3
		for y := 0.0; y <= 0.4; y += 0.005 {
			for a := 0.0; a < 2*math.Pi; a += math.Pi / 24 {
				out = append(out, vec3{cx + r*math.Cos(a), y, cz + r*math.Sin(a)})
			}
		}
		return out
	}
	skin := tube(0.3, 0, 0.05)
	points := tube(0.28, 0.01, 0.025)
	var muscles []triangle
	var own []int
	for i := 0; i+2 < len(points); i += 3 {
		muscles = append(muscles, triangle{points[i], points[i+1], points[i+2]})
		own = append(own, len(muscles)-1)
	}
	sleeve(muscles, own, skin)
	for _, tri := range muscles {
		for _, p := range tri {
			if p[1] > 0.4-sleeveBlend {
				continue // eased in below the top
			}
			r := math.Hypot(p[0]-0.3, p[2])
			if r < 0.04 || r > 0.05 {
				t.Fatalf("point %v is %.3f from the skin's axis, want just under 0.05", p, r)
			}
		}
	}
}

func TestTrimTendons(t *testing.T) {
	at := func(y float64) triangle { return triangle{{0, y, 0}, {0.01, y, 0}, {0, y, 0.01}} }
	muscles := []triangle{at(0.5), at(0.9), at(0.02), at(0.3), at(0.5)}
	keys := []string{"forearms", "forearms", "calves", "calves", "quads"}
	gotT, gotK := trimTendons(muscles, keys, 0.6, 0.08)
	if len(gotT) != 3 || len(gotK) != 3 {
		t.Fatalf("kept %d triangles, want 3", len(gotT))
	}
	want := []string{"forearms", "calves", "quads"}
	for i, k := range gotK {
		if k != want[i] {
			t.Errorf("kept %v, want %v", gotK, want)
			break
		}
	}
}
