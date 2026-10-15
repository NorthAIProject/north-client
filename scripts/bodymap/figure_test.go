package main

import "testing"

// Two rasters framed by the same bounds are the same size, whatever they
// contain, and a triangle labels the pixels it covers.
func TestRasteriseSharedBounds(t *testing.T) {
	lo, hi := vec3{0, 0, 0}, vec3{0.03, 0.03, 0.03}
	big := rasterise([]triangle{{{0, 0, 0}, {0.03, 0, 0}, {0, 0.03, 0}}}, []string{"shell"}, false, lo, hi)
	small := rasterise([]triangle{{{0.01, 0.01, 0}, {0.02, 0.01, 0}, {0.01, 0.02, 0}}}, []string{"quads"}, false, lo, hi)
	if big.w != small.w || big.h != small.h {
		t.Fatalf("sizes differ: %dx%d vs %dx%d", big.w, big.h, small.w, small.h)
	}
	if big.w != 10+2*figurePad {
		t.Errorf("width = %d, want %d", big.w, 10+2*figurePad)
	}
	// The big triangle covers the top-left corner of the body; the small one
	// does not. x = figurePad, y = figurePad + (hi.y - 0) / pixel ... the
	// corner at (0, 0.03) is pixel (pad, pad).
	if got := big.pixels[figurePad*big.w+figurePad]; got != 0 {
		t.Errorf("big raster corner = %d, want label 0", got)
	}
	if got := small.pixels[figurePad*small.w+figurePad]; got != -1 {
		t.Errorf("small raster corner = %d, want background", got)
	}
	if small.names[0] != "quads" {
		t.Errorf("names = %v", small.names)
	}
}

// Nearest triangle wins a pixel.
func TestRasteriseDepth(t *testing.T) {
	lo, hi := vec3{0, 0, 0}, vec3{0.03, 0.03, 0.03}
	tris := []triangle{
		{{0, 0, 0}, {0.03, 0, 0}, {0, 0.03, 0}},
		{{0, 0, 0.01}, {0.03, 0, 0.01}, {0, 0.03, 0.01}},
	}
	front := rasterise(tris, []string{"behind", "near"}, false, lo, hi)
	if got := front.names[front.pixels[figurePad*front.w+figurePad]]; got != "near" {
		t.Errorf("front view shows %q, want near", got)
	}
	back := rasterise(tris, []string{"behind", "near"}, true, lo, hi)
	at := figurePad*back.w + back.w - figurePad - 1 // the corner mirrors in x
	if got := back.names[back.pixels[at]]; got != "behind" {
		t.Errorf("back view shows %q, want behind", got)
	}
}
