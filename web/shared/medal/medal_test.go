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
	id := func(s string) string {
		return strings.SplitN(strings.SplitN(s, `<linearGradient id="`, 2)[1], `"`, 2)[0]
	}
	if id(one) == id(two) {
		t.Errorf("two coins share gradient id %s", id(one))
	}
	if !strings.Contains(one, "url(#"+id(one)+")") {
		t.Error("fill must reference its own id")
	}
}

func renderMonthly(t *testing.T, s medals.Set, p medal.MonthlyProps) string {
	t.Helper()
	var b bytes.Buffer
	if err := medal.CoinWithMonthly(s, p).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestMonthlyCoinText(t *testing.T) {
	p := medal.MonthlyProps{Month: "OCT", Target: 18, Size: 96, Label: "October: 18 days"}
	var b bytes.Buffer
	if err := medal.MonthlyCoin(p).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{">OCT</text>", ">18</text>", `role="img"`, `aria-label="October: 18 days"`, `class="scallop"`, `text-anchor="middle"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	if strings.Contains(out, "opacity") || strings.Contains(out, "stroke-linecap") {
		t.Errorf("unlocked monthly coin must have no locked style or glyph: %s", out)
	}
	p.Locked = true
	b.Reset()
	_ = medal.MonthlyCoin(p).Render(context.Background(), &b)
	if !strings.Contains(b.String(), "grayscale(1);opacity:0.4") {
		t.Errorf("locked monthly coin needs the locked style: %s", b.String())
	}
}

func TestMonthlyCoinGeometryFromJSON(t *testing.T) {
	s := medals.Load()
	p := medal.MonthlyProps{Month: "NOV", Target: 5, Size: 96}
	s.Monthly.Fill = "#123456"
	s.Monthly.Text.Color = "#ABCDEF"
	s.Monthly.Text.MonthSize = 11
	s.Monthly.Text.MonthY = 33
	s.Monthly.Text.NumberSize = 29
	s.Monthly.Text.NumberY = 66
	s.Monthly.Text.FontWeight = 600
	out := renderMonthly(t, s, p)
	for _, want := range []string{`fill="#123456"`, `fill="#ABCDEF"`, `font-size="11"`, `y="33"`, `font-size="29"`, `y="66"`, `font-weight="600"`} {
		if !strings.Contains(out, want) {
			t.Errorf("json value %s not in output: %s", want, out)
		}
	}
}

func TestMonthlyTextContrast(t *testing.T) {
	m := medals.Load().Monthly
	ratio := (lum(m.Text.Color) + 0.05) / (lum(m.Fill) + 0.05)
	if ratio < 4.5 {
		t.Errorf("%s on %s is %.2f:1, want >= 4.5", m.Text.Color, m.Fill, ratio)
	}
}

// Text must sit inside the face: the widest line at its baseline row has to fit
// the chord of the face circle there, for one, two and three digit targets.
func TestMonthlyTextFitsTheFace(t *testing.T) {
	s := medals.Load()
	const digitWidth = 0.65 // em, wider than any digit in the system fonts
	mid := s.Coin.ViewBox / 2
	chord := func(y float64) float64 {
		d := y - mid
		return 2 * math.Sqrt(s.Coin.FaceRadius*s.Coin.FaceRadius-d*d)
	}
	for _, target := range []int{5, 27, 100, 365} {
		out := renderMonthly(t, s, medal.MonthlyProps{Month: "DEC", Target: target, Size: 96})
		size := s.Monthly.Text.NumberSize
		if target >= s.Monthly.Text.NumberWideFrom {
			size *= s.Monthly.Text.NumberWideScale
		}
		if !strings.Contains(out, `font-size="`+strconv.FormatFloat(size, 'f', -1, 64)+`"`) {
			t.Errorf("target %d: expected number font-size %v in %s", target, size, out)
		}
		width := float64(len(strconv.Itoa(target))) * digitWidth * size
		if limit := chord(s.Monthly.Text.NumberY); width > limit {
			t.Errorf("target %d: text %.1f wide, chord at baseline %.1f", target, width, limit)
		}
	}
	mw := 3 * 0.75 * s.Monthly.Text.MonthSize
	if limit := chord(s.Monthly.Text.MonthY - s.Monthly.Text.MonthSize*0.7); mw > limit {
		t.Errorf("month text %.1f wide, chord %.1f", mw, limit)
	}
}

func TestMonthlyWideNumberFollowsJSON(t *testing.T) {
	s := medals.Load()
	wide := func() string {
		return renderMonthly(t, s, medal.MonthlyProps{Month: "OCT", Target: 100, Size: 96})
	}
	s.Monthly.Text.NumberWideScale = 0.5
	if out := wide(); !strings.Contains(out, `font-size="17"`) {
		t.Errorf("scale 0.5 of 34 should give 17: %s", out)
	}
	s.Monthly.Text.NumberWideFrom = 101
	if out := wide(); !strings.Contains(out, `font-size="34"`) {
		t.Errorf("100 below the threshold 101 must use the full size: %s", out)
	}
	s.Monthly.Text.NumberWideFrom = 10
	out := renderMonthly(t, s, medal.MonthlyProps{Month: "OCT", Target: 18, Size: 96})
	if !strings.Contains(out, `font-size="17"`) {
		t.Errorf("18 at threshold 10 must be scaled: %s", out)
	}
}

func TestMonthlyTextIntentFollowsJSON(t *testing.T) {
	s := medals.Load()
	s.Monthly.Text.Anchor = "start"
	s.Monthly.Text.Baseline = false
	s.Monthly.Text.FontFamily = "unknown"
	out := renderMonthly(t, s, medal.MonthlyProps{Month: "OCT", Target: 18, Size: 96})
	for _, want := range []string{`text-anchor="start"`, `dominant-baseline="central"`, `font-family="system-ui,sans-serif"`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in %s", want, out)
		}
	}
	d := renderMonthly(t, medals.Load(), medal.MonthlyProps{Month: "OCT", Target: 18, Size: 96})
	for _, want := range []string{`text-anchor="middle"`, `dominant-baseline="alphabetic"`, "ui-rounded"} {
		if !strings.Contains(d, want) {
			t.Errorf("default missing %s in %s", want, d)
		}
	}
}
