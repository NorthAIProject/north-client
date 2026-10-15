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
