// Command pwa-icons paints the Khepri mark onto opaque dark plates at the
// sizes an installable PWA needs.
//
// iOS apple-touch-icon and Android maskable icons treat transparency as
// black or crop it away, so the plates use the same void as the dark shell
// (#1C1C1F) rather than shipping the raw mark.
//
// Run it from the repository root:
//
//	go run ./scripts/pwa-icons
package main

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"os"
	"path/filepath"
)

// Matches the dark shell documented in web/assets/css/input.css
// ("Void: cool night navigation (~#1C1C1F)").
var bg = color.RGBA{R: 0x1C, G: 0x1C, B: 0x1F, A: 0xFF}

func main() {
	src := mustDecode(filepath.Join("web", "assets", "brand", "khepri-logo-mark.png"))
	write(filepath.Join("web", "assets", "brand", "pwa-180.png"), composite(src, 180, 0.12))
	write(filepath.Join("web", "assets", "brand", "pwa-192.png"), composite(src, 192, 0.12))
	write(filepath.Join("web", "assets", "brand", "pwa-512.png"), composite(src, 512, 0.12))
	write(filepath.Join("web", "assets", "brand", "pwa-512-maskable.png"), composite(src, 512, 0.22))
}

// composite draws src contained in a size×size plate, padded by padFrac of
// the edge so rounded-square and adaptive-icon masks do not clip the needle.
func composite(src image.Image, size int, padFrac float64) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: bg}, image.Point{}, draw.Src)

	pad := int(float64(size) * padFrac)
	inner := size - 2*pad
	if inner < 1 {
		return dst
	}

	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	scale := float64(inner) / float64(sw)
	if float64(sh)*scale > float64(inner) {
		scale = float64(inner) / float64(sh)
	}
	dw := max(1, int(float64(sw)*scale))
	dh := max(1, int(float64(sh)*scale))
	ox := pad + (inner-dw)/2
	oy := pad + (inner-dh)/2

	scaled := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		sy := sb.Min.Y + y*sh/dh
		for x := 0; x < dw; x++ {
			sx := sb.Min.X + x*sw/dw
			scaled.Set(x, y, src.At(sx, sy))
		}
	}
	draw.Draw(dst, image.Rect(ox, oy, ox+dw, oy+dh), scaled, image.Point{}, draw.Over)
	return dst
}

func mustDecode(path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		log.Fatal(err)
	}
	img, err := png.Decode(f)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		log.Fatalf("decode %s: %v", path, err)
	}
	return img
}

func write(path string, img image.Image) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		log.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	err = png.Encode(f, img)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		log.Fatalf("encode %s: %v", path, err)
	}
}
