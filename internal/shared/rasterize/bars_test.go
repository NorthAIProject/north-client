package rasterize

import (
	"image"
	"image/draw"
	"testing"
)

func barCanvas() (*image.RGBA, image.Rectangle) {
	img := image.NewRGBA(image.Rect(0, 0, 300, 120))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: colorBackground}, image.Point{}, draw.Src)
	return img, image.Rect(10, 10, 290, 110)
}

// signalPixels counts how much of a column the bar actually fills.
func signalPixels(img *image.RGBA, area image.Rectangle, index, count int) int {
	slot := area.Dx() / count
	x := area.Min.X + slot*index + slot/2

	n := 0
	for y := area.Min.Y; y < area.Max.Y; y++ {
		if near(at(img, x, y), colorSignal) {
			n++
		}
	}
	return n
}

func TestBarsAreProportionalToTheirValues(t *testing.T) {
	img, area := barCanvas()
	drawBars(img, area, []float64{10, 5})

	tall := signalPixels(img, area, 0, 2)
	half := signalPixels(img, area, 1, 2)

	if tall == 0 || half == 0 {
		t.Fatalf("bars did not draw: %d and %d", tall, half)
	}
	if tall <= half {
		t.Errorf("value 10 drew %d px, value 5 drew %d px — want the first taller", tall, half)
	}
}

func TestBarsLeaveAnEmptyValueEmpty(t *testing.T) {
	// A day nobody logged must read as absent, not as a one-pixel sliver that
	// looks like a very bad day.
	img, area := barCanvas()
	drawBars(img, area, []float64{10, 0})

	if got := signalPixels(img, area, 1, 2); got != 0 {
		t.Errorf("a zero value drew %d px, want none", got)
	}
}

func TestBarsSurviveAWindowOfNothing(t *testing.T) {
	// Every value zero must not divide by a zero maximum.
	img, area := barCanvas()
	drawBars(img, area, []float64{0, 0, 0})

	for i := 0; i < 3; i++ {
		if got := signalPixels(img, area, i, 3); got != 0 {
			t.Errorf("bar %d drew %d px on an empty window", i, got)
		}
	}
}

func TestBarsStayInsideTheirArea(t *testing.T) {
	img, area := barCanvas()
	drawBars(img, area, []float64{10, 10, 10})

	// One row above the area and one below must be untouched ground.
	for _, y := range []int{area.Min.Y - 2, area.Max.Y + 2} {
		for x := area.Min.X; x < area.Max.X; x++ {
			if near(at(img, x, y), colorSignal) {
				t.Fatalf("painted outside the area at (%d, %d)", x, y)
			}
		}
	}
}

func TestBarsOfNoValuesDrawNothing(t *testing.T) {
	img, area := barCanvas()
	drawBars(img, area, nil)

	for x := area.Min.X; x < area.Max.X; x++ {
		for y := area.Min.Y; y < area.Max.Y; y++ {
			if near(at(img, x, y), colorSignal) {
				t.Fatalf("painted something for an empty series at (%d, %d)", x, y)
			}
		}
	}
}
