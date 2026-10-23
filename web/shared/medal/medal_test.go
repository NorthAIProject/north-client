package medal_test

import (
	"bytes"
	"context"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/docs/medals"
	"github.com/NorthAIProject/north-client/internal/awards/award"
	"github.com/NorthAIProject/north-client/web/shared/medal"
)

func render(t *testing.T, p medal.Props) string {
	t.Helper()
	var b bytes.Buffer
	if err := medal.Coin(p).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestEveryBadgeHasAGlyph(t *testing.T) {
	set := medals.Load()
	for _, b := range award.Catalog() {
		if len(set.Glyphs[b.ID]) == 0 {
			t.Errorf("badge %s has no glyph in medals.json", b.ID)
		}
		if _, ok := set.Areas[string(b.Area)]; !ok {
			t.Errorf("area %s has no colours in medals.json", b.Area)
		}
	}
	if len(set.Glyphs) != len(award.Catalog()) {
		t.Errorf("medals.json has %d glyphs, catalog has %d", len(set.Glyphs), len(award.Catalog()))
	}
}

func TestCoinRendersLockedAndMonthly(t *testing.T) {
	base := medal.Props{BadgeID: "streak_7", Area: award.Consistency, Size: 96, Label: "Seven days"}
	plain := render(t, base)
	if strings.Contains(plain, "scallop") || strings.Contains(plain, "opacity") {
		t.Errorf("plain coin must have no scallop or opacity: %s", plain)
	}
	if !strings.Contains(plain, `role="img"`) || !strings.Contains(plain, `aria-label="Seven days"`) {
		t.Errorf("coin needs role and aria-label: %s", plain)
	}
	m := base
	m.Monthly = true
	if !strings.Contains(render(t, m), "scallop") {
		t.Error("monthly coin needs the scalloped rim")
	}
	l := base
	l.Locked = true
	if !strings.Contains(render(t, l), "opacity") {
		t.Error("locked coin needs opacity")
	}
}

func lum(hex string) float64 {
	var c [3]float64
	for i := range c {
		v, _ := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		x := float64(v) / 255
		if x <= 0.03928 {
			c[i] = x / 12.92
		} else {
			c[i] = math.Pow((x+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2]
}

// The face is the same in light and dark, so one ratio covers both themes.
func TestGlyphContrastOnEveryFace(t *testing.T) {
	for area, c := range medals.Load().Areas {
		ratio := 1.05 / (lum(c.Fill) + 0.05)
		if ratio < 3 {
			t.Errorf("%s: white on %s is %.2f:1, want >= 3", area, c.Fill, ratio)
		}
	}
}

func TestGeometryComesFromJSON(t *testing.T) {
	s := medals.Load()
	p := medal.Props{BadgeID: "streak_7", Area: award.Consistency, Size: 96, Monthly: true}
	var a, b bytes.Buffer
	_ = medal.CoinWith(s, p).Render(context.Background(), &a)
	s.Coin.Glyph.Scale = 1.5
	s.Coin.FaceRadius = 33
	s.Coin.Scallop.Bumps = 7
	_ = medal.CoinWith(s, p).Render(context.Background(), &b)
	if !strings.Contains(b.String(), "scale(1.5)") || !strings.Contains(b.String(), `r="33"`) {
		t.Errorf("changed geometry not in output: %s", b.String())
	}
	if !strings.Contains(a.String(), "scale(1.9)") || a.String() == b.String() {
		t.Error("output must differ with the json values")
	}
}

func TestEachCoinGetsItsOwnGradientID(t *testing.T) {
	p := medal.Props{BadgeID: "streak_7", Area: award.Consistency, Size: 40}
	one, two := render(t, p), render(t, p)
	id := func(s string) string { return strings.SplitN(strings.SplitN(s, `<linearGradient id="`, 2)[1], `"`, 2)[0] }
	if id(one) == id(two) {
		t.Errorf("two coins share gradient id %s", id(one))
	}
	if !strings.Contains(one, "url(#"+id(one)+")") {
		t.Error("fill must reference its own id")
	}
}
