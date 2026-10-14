package landing

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

func renderLandingIn(t *testing.T, locale string) string {
	t.Helper()
	var b strings.Builder
	if err := Page().Render(i18n.WithLocale(context.Background(), locale), &b); err != nil {
		t.Fatalf("render %s: %v", locale, err)
	}
	return b.String()
}

// The demos and product mock-ups are most of the page. A Spanish visitor who
// reads a Spanish hero and then an English muscle viewer has been sold a
// translation that stops at the fold.
func TestLandingDemosRenderInTheVisitorsLanguage(t *testing.T) {
	out := renderLandingIn(t, "es")

	for _, want := range []string{
		"Mira dónde cae de verdad el trabajo.", // muscle viewer heading
		"Peso muerto rumano",                   // exercise tab
		"Isquiotibiales",                       // muscle readout
		"Todo tu día en una sola página.",      // day mock-up
		"Pide un plan. Recibe un plan, ya guardado.",
		"Cuatro apps. Una conversación. Nada repetido.",
		"Empezar este plan",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("es landing is missing %q", want)
		}
	}
	for _, english := range []string{
		"See where the work actually lands.",
		"Romanian deadlift",
		"Your whole day on one page.",
		"Ask for a plan. Get a plan, saved.",
		"Start this plan",
	} {
		if strings.Contains(out, english) {
			t.Errorf("es landing still shows the English %q", english)
		}
	}
}

// The goal, BMI and plan-builder demos write their sentences in the browser.
// The words they use have to arrive with the page, in the page's language.
func TestLandingShipsScriptCopyInTheVisitorsLanguage(t *testing.T) {
	out := renderLandingIn(t, "pt-PT")

	m := regexp.MustCompile(`(?s)<script[^>]*id="north-landing-copy"[^>]*>(.*?)</script>`).FindStringSubmatch(out)
	if m == nil {
		t.Fatal("landing renders no script copy")
	}
	var copy map[string]string
	if err := json.Unmarshal([]byte(m[1]), &copy); err != nil {
		t.Fatalf("script copy is not JSON: %v", err)
	}
	if got, want := copy["land.goal.horizon"], "Doze semanas, revistas a cada três"; got != want {
		t.Errorf("land.goal.horizon = %q, want %q", got, want)
	}
	if got := copy["land.bmi.decimal"]; got != "," {
		t.Errorf("land.bmi.decimal = %q, want a comma", got)
	}
	if _, leaked := copy["land.hero.h1a"]; leaked {
		t.Error("script copy carries strings the script never reads")
	}
}
