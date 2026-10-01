package day

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var b strings.Builder
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	return b.String()
}

func TestQuickAddOpensWithTheCaptureBox(t *testing.T) {
	t.Parallel()

	html := render(t, quickAdd(Data{CaffeinePresets: []Preset{{Key: "coffee", Value: "100 mg"}}}))

	capture := strings.Index(html, `data-testid="capture-embed"`)
	water := strings.Index(html, `action="/app/care/water"`)
	if capture < 0 || water < 0 || capture > water {
		t.Fatalf("the capture box is not the first section (capture at %d, water at %d)", capture, water)
	}
	for _, want := range []string{
		`hx-get="/app/capture/panel?return_to=%2Fapp"`,
		`hx-trigger="intersect once"`,
		`data-testid="quick-water-other"`,
		`max="5000"`,
		`data-testid="quick-caffeine-other"`,
		`name="mg"`,
		`name="label"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("quick add lacks %s", want)
		}
	}
}

func TestTheWaterCardTakesAnyAmount(t *testing.T) {
	t.Parallel()

	html := render(t, waterDetail(WaterCard{}))
	if !strings.Contains(html, `data-testid="day-water-other"`) || !strings.Contains(html, `name="amount_ml"`) {
		t.Error("the water card has no other-amount form")
	}
}
