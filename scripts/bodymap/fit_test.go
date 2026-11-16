package main

import (
	"math"
	"testing"
)

func TestAtlasFitInvert(t *testing.T) {
	fit := atlasFit{floor: vec3{0, 0.1, 0}, scale: 0.85, scaleX: 1.06, shiftY: 0.12, shiftZ: -0.01}
	p := vec3{0.2, 1.1, -0.05}
	if got := fit.invert(fit.apply(p)); got.sub(p).length() > 1e-9 {
		t.Errorf("invert(apply(p)) = %v, want %v", got, p)
	}
}

// Skin with muscle right under it costs nothing to speak of; skin with none
// in reach costs the full reach, and a bare half costs half that.
func TestCoverageCost(t *testing.T) {
	muscle := []triangle{{{0, 0, 0}, {0.1, 0, 0}, {0, 0.1, 0}}}
	same := func(p vec3) vec3 { return p }
	near := skinSample{at: vec3{0.02, 0.02, 0.001}, area: 1}
	far := skinSample{at: vec3{0.5, 0.5, 0.5}, area: 1}

	if got := newCoverage(muscle, []skinSample{near}).cost(same); got > 1e-5 {
		t.Errorf("covered skin costs %g", got)
	}
	bare := coverReach * coverReach
	if got := newCoverage(muscle, []skinSample{far}).cost(same); math.Abs(got-bare) > 1e-12 {
		t.Errorf("bare skin costs %g, want %g", got, bare)
	}
	got := newCoverage(muscle, []skinSample{near, far}).cost(same)
	if math.Abs(got-bare/2) > 1e-5 {
		t.Errorf("half-bare skin costs %g, want about %g", got, bare/2)
	}
	// Area weighs: the same bare point standing for three times the skin.
	far.area = 3
	got = newCoverage(muscle, []skinSample{near, far}).cost(same)
	if math.Abs(got-bare*3/4) > 1e-5 {
		t.Errorf("weighted cost %g, want about %g", got, bare*3/4)
	}
}
