// Command exercise-loops builds one looping GIF per exercise from the Workout
// Guide pose artwork, for the channels that cannot render an SVG.
//
// The web app animates the three SVG frames with CSS and Alpine
// (web/shared/exerciseart), which is the right answer in a browser. Telegram
// has no such option: sendPhoto and sendAnimation take raster formats, and an
// SVG is rejected. So the same three frames are baked into a GIF here, once,
// offline — not rasterised per request. The input never changes, and a
// conversion in the request path would be CPU and a failure mode bought for
// nothing.
//
// Run it from the repository root, against a shallow clone:
//
//	git clone --depth 1 https://github.com/bryllim/workout-guide /tmp/workout-guide
//	go run ./scripts/exercise-loops \
//	  -source /tmp/workout-guide \
//	  -assets web/assets/exercises
//
// It reads upstream's PNG frames rather than the SVGs this repository ships.
// Upstream renders both from the same source, so the pixels are theirs and no
// SVG rasteriser is needed — which is why this command has no dependency
// outside the standard library.
//
// Only exercises already present under -assets are written, so this cannot
// introduce artwork the catalog has no row for. See workout-guide-art, which
// decides that.
//
// # Licensing
//
// The artwork is CC BY-SA 4.0 (upstream's LICENSE-ASSETS; its *code* is MIT,
// which is not the file that governs here), tracing back to Everkinetic.
//
// Unlike workout-guide-art, this command *does* adapt the work: the frames are
// composited onto an opaque background and quantised to a grey ramp. ShareAlike
// therefore applies to the output and the changes must be declared. Both are
// recorded in the NOTICE beside the assets. Do not drop that file, and do not
// let a caption ship the picture without the credit.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"sort"
)

// frameCount matches web/shared/exerciseart, where the same three frames are
// cycled in the browser. Upstream ships exactly three for every exercise.
const frameCount = 3

// frameDelay is hundredths of a second, so 90 is the 900ms the web animation
// already uses. Matching it means the movement reads at the same speed
// wherever somebody meets it.
const frameDelay = 90

// background is the app's own theme colour, the same value as the
// theme-color meta tag and the OG card.
//
// Baking it in is the whole reason this command exists rather than a plain
// format conversion. The frames are white artwork on transparency, which is
// why the web renders them as a CSS mask: an <img> of one is invisible against
// anything pale. A GIF with the transparency preserved arrives in Telegram as
// a blank rectangle on its light background.
var background = color.RGBA{R: 0x1C, G: 0x1C, B: 0x1F, A: 0xFF}

// shades is the size of the grey ramp the frames are quantised to.
//
// Eight, which looks identical to 64 at any size this is displayed: the
// artwork is a hard white line over one flat background, so almost the whole
// ramp would be spent on steps that never occur. Measured on the pull-up loop,
// 64 shades cost 41 KB and 8 cost 22 KB for the same picture — across 302
// exercises that is the difference between 21 MB and 11 MB embedded in the
// binary.
const shades = 8

func main() {
	source := flag.String("source", "", "path to a workout-guide checkout")
	assets := flag.String("assets", "web/assets/exercises", "directory holding the exercise frames")
	flag.Parse()

	if *source == "" {
		log.Fatal("usage: exercise-loops -source <workout-guide checkout> [-assets dir]")
	}

	slugs, err := shippedSlugs(*assets)
	if err != nil {
		log.Fatalf("reading %s: %v", *assets, err)
	}
	if len(slugs) == 0 {
		log.Fatalf("no exercise directories under %s", *assets)
	}

	ramp := greyRamp()

	var written, skipped int
	var missing []string

	for _, slug := range slugs {
		frames, err := readFrames(*source, slug)
		if err != nil {
			// An exercise whose frames upstream no longer ships is reported
			// rather than fatal: the catalog is allowed to be ahead of the
			// artwork, and one gap should not cost the other 301 loops.
			missing = append(missing, slug)
			skipped++
			continue
		}

		out := filepath.Join(*assets, slug, "loop.gif")
		if err := writeLoop(out, frames, ramp); err != nil {
			log.Fatalf("writing %s: %v", out, err)
		}
		written++
	}

	fmt.Printf("wrote %d loops, skipped %d\n", written, skipped)
	if len(missing) > 0 {
		fmt.Printf("no upstream frames for: %v\n", missing)
	}
}

// shippedSlugs lists the exercises this repository already carries artwork for.
func shippedSlugs(assets string) ([]string, error) {
	entries, err := os.ReadDir(assets)
	if err != nil {
		return nil, err
	}

	var slugs []string
	for _, e := range entries {
		if e.IsDir() {
			slugs = append(slugs, e.Name())
		}
	}
	sort.Strings(slugs)
	return slugs, nil
}

// readFrames decodes upstream's three PNGs for one exercise.
func readFrames(source, slug string) ([]image.Image, error) {
	dir := filepath.Join(source, "packages", "workout-guide", "assets", slug)

	frames := make([]image.Image, 0, frameCount)
	for i := 1; i <= frameCount; i++ {
		f, err := os.Open(filepath.Join(dir, fmt.Sprintf("frame-%d.png", i)))
		if err != nil {
			return nil, err
		}
		img, err := png.Decode(f)
		_ = f.Close()
		if err != nil {
			return nil, fmt.Errorf("frame %d: %w", i, err)
		}
		frames = append(frames, img)
	}
	return frames, nil
}

// greyRamp builds the palette: the background at one end, white at the other.
//
// Index 0 is the background exactly, so a fully transparent pixel lands on it
// with no rounding.
func greyRamp() color.Palette {
	p := make(color.Palette, 0, shades)
	for i := range shades {
		t := float64(i) / float64(shades-1)
		mix := func(from, to uint8) uint8 {
			return uint8(float64(from) + (float64(to)-float64(from))*t)
		}
		p = append(p, color.RGBA{
			R: mix(background.R, 0xFF),
			G: mix(background.G, 0xFF),
			B: mix(background.B, 0xFF),
			A: 0xFF,
		})
	}
	return p
}

// writeLoop composites each frame onto the background and encodes the loop.
func writeLoop(path string, frames []image.Image, ramp color.Palette) error {
	out := &gif.GIF{LoopCount: 0} // 0 is "forever"

	for _, frame := range frames {
		bounds := frame.Bounds()

		// Draw.Src onto an opaque fill, so the alpha is resolved here rather
		// than carried into the GIF, where it would become a transparent index
		// and show the recipient's own background through the artwork.
		flat := image.NewRGBA(bounds)
		draw.Draw(flat, bounds, &image.Uniform{C: background}, image.Point{}, draw.Src)
		draw.Draw(flat, bounds, frame, bounds.Min, draw.Over)

		// Nearest colour, not Floyd-Steinberg. Dithering is the reflex here
		// and it is wrong for this input: it buys nothing visible on a hard
		// edge that is already antialiased, and it was measured at 41,102
		// bytes against 40,837 without — noise that defeats LZW for no gain.
		paletted := image.NewPaletted(bounds, ramp)
		draw.Draw(paletted, bounds, flat, image.Point{}, draw.Src)

		// No Disposal: every frame is opaque and covers the whole canvas, so
		// there is nothing to clear between them, and asking for a clear makes
		// some renderers flash the background.
		out.Image = append(out.Image, paletted)
		out.Delay = append(out.Delay, frameDelay)
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := gif.EncodeAll(f, out); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
