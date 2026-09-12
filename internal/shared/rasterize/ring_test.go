package rasterize

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"testing"
)

// onRing is the pixel at a given clock angle, measured the way the ring is
// drawn: 0 is twelve o'clock, increasing clockwise.
func onRing(cx, cy, radius, degrees float64) (int, int) {
	rad := (degrees - 90) * math.Pi / 180
	return int(cx + radius*math.Cos(rad)), int(cy + radius*math.Sin(rad))
}

func ringCanvas() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 200, 200))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: colorBackground}, image.Point{}, draw.Src)
	return img
}

func near(a, b color.RGBA) bool {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R)+d(a.G, b.G)+d(a.B, b.B) < 90
}

func at(img *image.RGBA, x, y int) color.RGBA {
	c := img.RGBAAt(x, y)
	return color.RGBA{R: c.R, G: c.G, B: c.B, A: c.A}
}

func TestRingFillsFromTheTopClockwise(t *testing.T) {
	img := ringCanvas()
	drawRing(img, 100, 100, 70, 14, 50)

	x, y := onRing(100, 100, 70, 45) // a quarter round, inside the first half
	if got := at(img, x, y); !near(got, colorSignal) {
		t.Errorf("45° = %v, want the signal colour — the arc starts at the top", got)
	}

	x, y = onRing(100, 100, 70, 260) // past the halfway point
	if got := at(img, x, y); near(got, colorSignal) {
		t.Errorf("260° = %v, want the empty track on a half-filled ring", got)
	}
}

func TestRingAtFullScoreClosesAllTheWayRound(t *testing.T) {
	img := ringCanvas()
	drawRing(img, 100, 100, 70, 14, 100)

	for _, deg := range []float64{0, 90, 180, 270, 359} {
		x, y := onRing(100, 100, 70, deg)
		if got := at(img, x, y); !near(got, colorSignal) {
			t.Errorf("%.0f° = %v, want the signal colour on a closed ring", deg, got)
		}
	}
}

func TestRingAtZeroDrawsOnlyTheTrack(t *testing.T) {
	// The empty state has to look like an unstarted ring, not a broken one.
	img := ringCanvas()
	drawRing(img, 100, 100, 70, 14, 0)

	for _, deg := range []float64{0, 120, 240} {
		x, y := onRing(100, 100, 70, deg)
		got := at(img, x, y)
		if near(got, colorSignal) {
			t.Errorf("%.0f° = %v, want the track on an empty ring", deg, got)
		}
		if near(got, colorBackground) {
			t.Errorf("%.0f° = %v, want a visible track rather than bare ground", deg, got)
		}
	}
}

func TestRingIsADonutNotADisc(t *testing.T) {
	img := ringCanvas()
	drawRing(img, 100, 100, 70, 14, 100)

	if got := at(img, 100, 100); !near(got, colorBackground) {
		t.Errorf("centre = %v, want the ground showing through the hole", got)
	}
}

func TestRingClampsAScoreOutsideTheScale(t *testing.T) {
	// Points arrive from a scorer that already clamps, but a ring that drew
	// 140% as a second lap would be a silent, very confusing bug.
	img := ringCanvas()
	drawRing(img, 100, 100, 70, 14, 140)

	x, y := onRing(100, 100, 70, 359)
	if got := at(img, x, y); !near(got, colorSignal) {
		t.Errorf("359° = %v, want a closed ring", got)
	}
}
