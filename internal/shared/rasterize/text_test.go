package rasterize

import (
	"image"
	"image/draw"
	"testing"
)

func textCanvas() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 400, 100))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: colorBackground}, image.Point{}, draw.Src)
	return img
}

func painted(img *image.RGBA) int {
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if !near(at(img, x, y), colorBackground) {
				n++
			}
		}
	}
	return n
}

func TestTextPaintsGlyphs(t *testing.T) {
	img := textCanvas()
	drawText(img, 10, 50, "Last 7 days", colorForeground, textRegular, 24)

	if painted(img) == 0 {
		t.Error("nothing was drawn — the font did not load or the baseline is off-canvas")
	}
}

func TestTextOfNothingDrawsNothing(t *testing.T) {
	img := textCanvas()
	drawText(img, 10, 50, "", colorForeground, textRegular, 24)

	if got := painted(img); got != 0 {
		t.Errorf("an empty string painted %d pixels", got)
	}
}

func TestTextWidthGrowsWithTheString(t *testing.T) {
	short := textWidth("7", textRegular, 24)
	long := textWidth("Last 7 days", textRegular, 24)

	if short <= 0 {
		t.Fatalf("width of a single character = %d, want more than nothing", short)
	}
	if long <= short {
		t.Errorf("width: %q=%d is not wider than %q=%d", "Last 7 days", long, "7", short)
	}
}

func TestBoldIsWiderThanRegularAtTheSameSize(t *testing.T) {
	// Not a typographic law, but it is true of this typeface, and it is the
	// cheapest assertion that proves two distinct faces actually loaded
	// rather than one being silently substituted for the other.
	regular := textWidth("Strong", textRegular, 24)
	bold := textWidth("Strong", textBold, 24)

	if bold <= regular {
		t.Errorf("bold %d is not wider than regular %d — the faces may be the same", bold, regular)
	}
}

func TestTextCentresOnAPoint(t *testing.T) {
	left := textCanvas()
	drawTextCentred(left, 200, 50, "88", colorForeground, textBold, 32)

	// The drawn ink should straddle the centre rather than start at it.
	minX, maxX := 400, 0
	b := left.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if !near(at(left, x, y), colorBackground) {
				if x < minX {
					minX = x
				}
				if x > maxX {
					maxX = x
				}
			}
		}
	}
	if minX > maxX {
		t.Fatal("nothing drawn")
	}

	mid := (minX + maxX) / 2
	if mid < 190 || mid > 210 {
		t.Errorf("ink centred at %d, want it near 200", mid)
	}
}
