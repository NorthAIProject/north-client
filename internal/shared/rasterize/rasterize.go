// Package rasterize draws a digest card as a PNG, server-side.
//
// Separate from internal/shared/viz, which builds option JSON for a browser to
// render. Nothing here runs in a browser: the worker has no DOM, no canvas and
// no headless Chrome, and putting a rasteriser inside viz would make that one
// package mean two different things.
//
// Deliberately small. It draws the two shapes a digest needs — a score ring and
// a column chart — and nothing else. Anything richer belongs on the web page,
// which the digest links to.
package rasterize

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
)

// Card is one rendered digest picture.
type Card struct {
	Title    string
	Subtitle string

	Rings []Ring
	Bars  Bars
}

// Ring is one domain's score.
type Ring struct {
	Label  string
	Points int

	// HasData false draws the empty track and no number, matching what the
	// web page does rather than drawing a zero nobody measured.
	HasData bool
}

// Bars is a single labelled column series.
type Bars struct {
	Label  string
	Values []float64
	Labels []string
}

// Card dimensions. Wide enough that Telegram does not downscale it into
// illegibility on a phone, small enough to stay well inside the upload limit.
const (
	CardWidth  = 1000
	CardHeight = 600
)

// The dark palette, as literal colours.
//
// This is a real duplication of web/assets/css/input.css and is named as such:
// those values are OKLCH custom properties resolved by a browser, and there is
// no browser here to resolve them. Changing the app's ground colour means
// changing it in both places — which is the cost of drawing the same product
// in two renderers.
var (
	colorBackground = color.RGBA{R: 0x0b, G: 0x0c, B: 0x0e, A: 0xff}
	colorSurface    = color.RGBA{R: 0x14, G: 0x16, B: 0x19, A: 0xff}
	colorHairline   = color.RGBA{R: 0x26, G: 0x2a, B: 0x2f, A: 0xff}
	colorForeground = color.RGBA{R: 0xea, G: 0xec, B: 0xef, A: 0xff}
	colorMuted      = color.RGBA{R: 0x8a, G: 0x91, B: 0x9b, A: 0xff}
	colorSignal     = color.RGBA{R: 0x4a, G: 0xd2, B: 0x95, A: 0xff}
)

// RenderCard draws the card and encodes it as a PNG.
//
// Deterministic: the same card produces the same bytes every time. A digest
// whose picture changed between two identical sweeps would be untraceable.
func RenderCard(c Card) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, CardWidth, CardHeight))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: colorBackground}, image.Point{}, draw.Src)

	drawCard(img, c)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
