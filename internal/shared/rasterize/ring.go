package rasterize

import (
	"image"
	"image/color"
	"math"
)

// supersample is how many samples each pixel is tested at, per axis.
//
// A ring drawn by testing one point per pixel has a visibly jagged edge at
// this size. Four samples each way is sixteen tests per pixel, which is cheap
// at a thousand pixels wide and removes the stepping entirely.
const supersample = 4

// drawRing paints a donut arc filled clockwise from twelve o'clock.
//
// Drawn by scanning the bounding box and asking each sample whether it falls
// inside the annulus and before the filled angle, rather than by stroking a
// path. The shape is simple enough that the scan is shorter than any path
// rasteriser would be, and it antialiases by counting samples rather than by
// needing a coverage buffer.
func drawRing(img *image.RGBA, cx, cy, radius, thickness float64, percent int) {
	percent = clamp(percent, 0, 100)

	inner := radius - thickness/2
	outer := radius + thickness/2
	filledTo := float64(percent) / 100 * 2 * math.Pi

	minX, maxX := int(cx-outer)-1, int(cx+outer)+1
	minY, maxY := int(cy-outer)-1, int(cy+outer)+1

	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			if !image.Pt(x, y).In(img.Bounds()) {
				continue
			}

			var onTrack, onArc int
			for sy := 0; sy < supersample; sy++ {
				for sx := 0; sx < supersample; sx++ {
					px := float64(x) + (float64(sx)+0.5)/supersample
					py := float64(y) + (float64(sy)+0.5)/supersample

					dx, dy := px-cx, py-cy
					dist := math.Hypot(dx, dy)
					if dist < inner || dist > outer {
						continue
					}
					onTrack++

					// atan2 measured from twelve o'clock, increasing clockwise,
					// so the arc grows the way a progress ring is read.
					angle := math.Atan2(dx, -dy)
					if angle < 0 {
						angle += 2 * math.Pi
					}
					if angle <= filledTo {
						onArc++
					}
				}
			}
			if onTrack == 0 {
				continue
			}

			total := float64(supersample * supersample)
			shade := colorHairline
			if onArc*2 >= onTrack {
				shade = colorSignal
			}
			blend(img, x, y, shade, float64(onTrack)/total)
		}
	}
}

// blend mixes a colour into a pixel by coverage, so an edge sample lightens
// rather than stepping.
func blend(img *image.RGBA, x, y int, c color.RGBA, coverage float64) {
	if coverage <= 0 {
		return
	}
	if coverage > 1 {
		coverage = 1
	}

	existing := img.RGBAAt(x, y)
	mix := func(from, to uint8) uint8 {
		return uint8(float64(from)*(1-coverage) + float64(to)*coverage)
	}
	img.SetRGBA(x, y, color.RGBA{
		R: mix(existing.R, c.R),
		G: mix(existing.G, c.G),
		B: mix(existing.B, c.B),
		A: 0xff,
	})
}

func clamp(v, lo, hi int) int {
	switch {
	case v < lo:
		return lo
	case v > hi:
		return hi
	default:
		return v
	}
}
