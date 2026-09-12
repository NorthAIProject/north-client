package rasterize

import (
	"fmt"
	"image"
)

// Card layout. Fixed rather than computed: there are two arrangements this
// ever draws — some rings and one series — and a layout engine to place them
// would be more code than the card itself.
const (
	cardPad = 48

	titleBaseline    = 84
	subtitleBaseline = 124

	ringBandTop = 156
	ringRadius  = 62.0
	ringStroke  = 15.0
	ringLabelY  = 300

	barsTop = 344
)

// Type sizes, in points at 72dpi.
const (
	sizeTitle    = 34
	sizeSubtitle = 20
	sizeRingNum  = 30
	sizeLabel    = 17
)

// maxRings is how many score rings fit across the card before they are too
// small to read on a phone. The digest never has more than five.
const maxRings = 5

// drawCard paints the whole card onto img.
func drawCard(img *image.RGBA, c Card) {
	drawText(img, cardPad, titleBaseline, c.Title, colorForeground, textBold, sizeTitle)
	drawText(img, cardPad, subtitleBaseline, c.Subtitle, colorMuted, textRegular, sizeSubtitle)

	drawRings(img, c.Rings)
	drawBarPanel(img, c.Bars)
}

func drawRings(img *image.RGBA, rings []Ring) {
	if len(rings) == 0 {
		return
	}
	if len(rings) > maxRings {
		rings = rings[:maxRings]
	}

	slot := float64(CardWidth-2*cardPad) / float64(len(rings))
	centreY := float64(ringBandTop) + ringRadius

	for i, r := range rings {
		centreX := float64(cardPad) + slot*float64(i) + slot/2

		// An unscored domain draws its empty track and no number, which is
		// what the web page shows. A zero would be a claim about the person
		// rather than about the data.
		points := 0
		if r.HasData {
			points = r.Points
		}
		drawRing(img, centreX, centreY, ringRadius, ringStroke, points)

		if r.HasData {
			drawTextCentred(img, int(centreX), int(centreY)+11,
				fmt.Sprintf("%d", r.Points), colorForeground, textBold, sizeRingNum)
		} else {
			drawTextCentred(img, int(centreX), int(centreY)+11,
				"—", colorMuted, textRegular, sizeRingNum)
		}

		drawTextCentred(img, int(centreX), ringLabelY, r.Label, colorMuted, textRegular, sizeLabel)
	}
}

func drawBarPanel(img *image.RGBA, bars Bars) {
	if len(bars.Values) == 0 {
		return
	}

	panel := image.Rect(cardPad, barsTop, CardWidth-cardPad, CardHeight-cardPad)
	drawRect(img, panel, colorSurface)

	drawText(img, panel.Min.X+20, panel.Min.Y+34, bars.Label, colorMuted, textRegular, sizeLabel)

	plot := image.Rect(panel.Min.X+20, panel.Min.Y+52, panel.Max.X-20, panel.Max.Y-34)
	drawBars(img, plot, bars.Values)

	// A rule under the columns, so a window of nothing still reads as a chart
	// with no data rather than as an empty box.
	drawRect(img, image.Rect(plot.Min.X, plot.Max.Y, plot.Max.X, plot.Max.Y+1), colorHairline)

	for i, label := range bars.Labels {
		if i >= len(bars.Values) {
			break
		}
		slot := float64(plot.Dx()) / float64(len(bars.Values))
		x := float64(plot.Min.X) + slot*float64(i) + slot/2
		drawTextCentred(img, int(x), panel.Max.Y-12, label, colorMuted, textRegular, 13)
	}
}
