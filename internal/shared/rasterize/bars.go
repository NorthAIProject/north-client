package rasterize

import (
	"image"
	"image/color"
	"image/draw"
)

// barGap is the share of each column's slot left as space between bars.
const barGap = 0.28

// drawBars paints a column per value, scaled to the tallest.
//
// Scaled to the window's own maximum rather than to a fixed ceiling: these
// charts sit beside their own number in the caption, and the question the
// picture answers is what the shape of the window was, not how it compares
// with somebody else's.
func drawBars(img *image.RGBA, area image.Rectangle, values []float64) {
	if len(values) == 0 || area.Empty() {
		return
	}

	max := 0.0
	for _, v := range values {
		if v > max {
			max = v
		}
	}
	// Every value zero is a real window — somebody logged nothing all week —
	// and it draws no bars rather than dividing by a maximum of nothing.
	if max <= 0 {
		return
	}

	slot := float64(area.Dx()) / float64(len(values))
	width := slot * (1 - barGap)

	for i, v := range values {
		if v <= 0 {
			continue
		}

		height := v / max * float64(area.Dy())
		left := float64(area.Min.X) + slot*float64(i) + (slot-width)/2

		bar := image.Rect(
			int(left),
			area.Max.Y-int(height),
			int(left+width),
			area.Max.Y,
		)
		draw.Draw(img, bar.Intersect(area), &image.Uniform{C: colorSignal}, image.Point{}, draw.Src)
	}
}

// drawRect fills a rectangle, for panels and rules.
func drawRect(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	draw.Draw(img, r.Intersect(img.Bounds()), &image.Uniform{C: c}, image.Point{}, draw.Src)
}
