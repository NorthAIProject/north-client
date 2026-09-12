package rasterize

import (
	"bytes"
	"image"
	"image/color"
	_ "image/png" // registers the PNG decoder for image.Decode
	"os"
	"testing"
)

func decode(t *testing.T, raw []byte) image.Image {
	t.Helper()
	img, format, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if format != "png" {
		t.Fatalf("format = %q, want png", format)
	}
	return img
}

func TestRenderCardProducesAPNGAtTheStatedSize(t *testing.T) {
	raw, err := RenderCard(Card{Title: "Last 7 days"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	img := decode(t, raw)
	if got := img.Bounds().Dx(); got != CardWidth {
		t.Errorf("width = %d, want %d", got, CardWidth)
	}
	if got := img.Bounds().Dy(); got != CardHeight {
		t.Errorf("height = %d, want %d", got, CardHeight)
	}
}

func TestRenderCardPaintsAnOpaqueBackground(t *testing.T) {
	// Telegram composites a photo onto its own chat background. A transparent
	// or unset canvas would show through as whatever that happens to be.
	raw, err := RenderCard(Card{Title: "Last 7 days"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	img := decode(t, raw)
	_, _, _, a := img.At(2, 2).RGBA()
	if a != 0xffff {
		t.Errorf("corner alpha = %d, want fully opaque", a)
	}
	if r, g, b, _ := img.At(2, 2).RGBA(); r == 0xffff && g == 0xffff && b == 0xffff {
		t.Error("corner is white; the card should carry the dark ground the app uses")
	}
}

func TestRenderCardIsDeterministic(t *testing.T) {
	// The same window must produce the same bytes. A digest that changed its
	// picture between two identical sweeps would be impossible to debug.
	card := Card{Title: "Last 7 days", Rings: []Ring{{Label: "Body", Points: 72, HasData: true}}}

	first, err := RenderCard(card)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	second, err := RenderCard(card)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if !bytes.Equal(first, second) {
		t.Error("two renders of the same card differ")
	}
}

// rgbaOf decodes to the concrete type the pixel helpers want.
func rgbaOf(t *testing.T, raw []byte) *image.RGBA {
	t.Helper()
	img := decode(t, raw)
	out := image.NewRGBA(img.Bounds())
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			out.Set(x, y, img.At(x, y))
		}
	}
	return out
}

func countNear(img *image.RGBA, want color.RGBA) int {
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if near(at(img, x, y), want) {
				n++
			}
		}
	}
	return n
}

func TestRenderCardDrawsARingPerDomain(t *testing.T) {
	one, err := RenderCard(Card{Title: "Last 7 days", Rings: []Ring{
		{Label: "Body", Points: 80, HasData: true},
	}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	three, err := RenderCard(Card{Title: "Last 7 days", Rings: []Ring{
		{Label: "Body", Points: 80, HasData: true},
		{Label: "Mind", Points: 80, HasData: true},
		{Label: "Training", Points: 80, HasData: true},
	}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if countNear(rgbaOf(t, three), colorSignal) <= countNear(rgbaOf(t, one), colorSignal) {
		t.Error("three rings did not paint more than one")
	}
}

func TestRenderCardDrawsAnUnscoredDomainAsAnEmptyTrack(t *testing.T) {
	raw, err := RenderCard(Card{Title: "Last 7 days", Rings: []Ring{
		{Label: "Nutrition", HasData: false},
	}})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	img := rgbaOf(t, raw)
	if countNear(img, colorHairline) == 0 {
		t.Error("no track drawn for an unscored domain")
	}
	if countNear(img, colorSignal) != 0 {
		t.Error("an unscored domain drew a filled arc")
	}
}

func TestRenderCardDrawsItsBars(t *testing.T) {
	withBars, err := RenderCard(Card{
		Title: "Last 7 days",
		Bars:  Bars{Label: "Sleep", Values: []float64{7, 8, 6, 8, 7, 9, 7}},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	without, err := RenderCard(Card{Title: "Last 7 days"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if countNear(rgbaOf(t, withBars), colorSignal) <= countNear(rgbaOf(t, without), colorSignal) {
		t.Error("the bar series painted nothing")
	}
}

func TestRenderCardWritesItsTitle(t *testing.T) {
	titled, err := RenderCard(Card{Title: "Last 7 days"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	untitled, err := RenderCard(Card{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if countNear(rgbaOf(t, titled), colorForeground) <= countNear(rgbaOf(t, untitled), colorForeground) {
		t.Error("the title was not drawn")
	}
}

func TestRenderCardStaysWithinTelegramsPhotoLimit(t *testing.T) {
	// Telegram rejects a photo over 10MB. A flat-colour card is nowhere near
	// that, and this is the assertion that notices if the size ever changes.
	raw, err := RenderCard(Card{
		Title:    "Last 12 months",
		Subtitle: "4 of 5 on track",
		Rings: []Ring{
			{Label: "Body", Points: 88, HasData: true},
			{Label: "Mind", Points: 71, HasData: true},
			{Label: "Progress", Points: 64, HasData: true},
			{Label: "Training", Points: 92, HasData: true},
			{Label: "Nutrition", HasData: false},
		},
		Bars: Bars{Label: "Sleep", Values: []float64{7, 8, 6, 8, 7, 9, 7, 6, 8}},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	if len(raw) > 1<<20 {
		t.Errorf("card is %d bytes, want well under a megabyte", len(raw))
	}
}

// TestWriteSampleCard writes a card to look at.
//
// Pixel assertions prove the shapes are where they should be; they cannot say
// whether the result is legible. Set RASTERIZE_SAMPLE to a path and run this
// to see one.
func TestWriteSampleCard(t *testing.T) {
	path := os.Getenv("RASTERIZE_SAMPLE")
	if path == "" {
		t.Skip("set RASTERIZE_SAMPLE to write a sample card")
	}

	raw, err := RenderCard(Card{
		Title:    "Last 7 days",
		Subtitle: "4 of 5 on track",
		Rings: []Ring{
			{Label: "Body", Points: 88, HasData: true},
			{Label: "Mind", Points: 71, HasData: true},
			{Label: "Progress", Points: 64, HasData: true},
			{Label: "Training", Points: 92, HasData: true},
			{Label: "Nutrition", HasData: false},
		},
		Bars: Bars{
			Label:  "Sleep, hours a night",
			Values: []float64{7.6, 8.3, 6.6, 8.1, 7.2, 0, 7.8},
			Labels: []string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"},
		},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	t.Logf("wrote %d bytes to %s", len(raw), path)
}
