package rasterize

import (
	"image"
	"image/color"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// The two faces this package draws with.
//
// golang.org/x/image is the one dependency this package adds, and it is here
// for exactly this: the standard library has image/png and image/draw but no
// font rasteriser at all, so there is no stdlib answer to "draw these words".
// It is maintained by the Go team under the same policy as golang.org/x/text,
// x/sync and x/crypto, all of which are already direct dependencies, and
// font/gofont ships the typeface as Go source — so no binary asset enters the
// repository to carry it.
type textFace int

const (
	textRegular textFace = iota
	textBold
)

// Faces are parsed once and cached by size. Parsing an OpenType face is not
// free and a card draws a dozen strings; a worker sweeping a thousand accounts
// would otherwise re-parse the typeface for every one of them.
var (
	faceMu    sync.Mutex
	faceCache = map[faceKey]font.Face{}

	parsedOnce sync.Once
	regularTTF *opentype.Font
	boldTTF    *opentype.Font
	parseErr   error
)

type faceKey struct {
	face textFace
	size float64
}

func loadFace(which textFace, size float64) (font.Face, error) {
	parsedOnce.Do(func() {
		if regularTTF, parseErr = opentype.Parse(goregular.TTF); parseErr != nil {
			return
		}
		boldTTF, parseErr = opentype.Parse(gobold.TTF)
	})
	if parseErr != nil {
		return nil, parseErr
	}

	faceMu.Lock()
	defer faceMu.Unlock()

	key := faceKey{face: which, size: size}
	if cached, ok := faceCache[key]; ok {
		return cached, nil
	}

	src := regularTTF
	if which == textBold {
		src = boldTTF
	}
	built, err := opentype.NewFace(src, &opentype.FaceOptions{
		Size: size,
		DPI:  72,
		// Full hinting, because these are small labels on a dark ground and
		// the unhinted stems disappear at digest sizes.
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, err
	}
	faceCache[key] = built
	return built, nil
}

// drawText paints a string with its baseline at (x, y).
//
// A font that will not load is drawn as nothing rather than returned as an
// error: a digest missing a label is worth sending, and a digest that fails to
// send because a typeface did not parse is not.
func drawText(img *image.RGBA, x, y int, text string, c color.RGBA, which textFace, size float64) {
	if text == "" {
		return
	}
	face, err := loadFace(which, size)
	if err != nil {
		return
	}

	(&font.Drawer{
		Dst:  img,
		Src:  &image.Uniform{C: c},
		Face: face,
		Dot:  fixed.P(x, y),
	}).DrawString(text)
}

// drawTextCentred paints a string centred horizontally on x.
func drawTextCentred(img *image.RGBA, x, y int, text string, c color.RGBA, which textFace, size float64) {
	drawText(img, x-textWidth(text, which, size)/2, y, text, c, which, size)
}

// textWidth measures a string in pixels, so a caller can centre or wrap it.
func textWidth(text string, which textFace, size float64) int {
	if text == "" {
		return 0
	}
	face, err := loadFace(which, size)
	if err != nil {
		return 0
	}
	return font.MeasureString(face, text).Round()
}
