package lighterday

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

func TestOfferRendersInTheChosenLanguage(t *testing.T) {
	cases := map[users.Locale][]string{
		users.LocaleEN:   {"Lighter day?", "Train about 60% of today's sets for Push A", "Keep the plan"},
		users.LocalePTPT: {"Dia mais leve?", "Faz cerca de 60% das séries de hoje de Push A", "Manter o plano"},
		users.LocalePTBR: {"Dia mais leve?", "Faça cerca de 60% das séries de hoje de Push A", "Pegar mais leve"},
		users.LocaleES:   {"¿Día más suave?", "Haz alrededor del 60% de las series de hoy de Push A", "Mantener el plan"},
	}
	for locale, wants := range cases {
		var b strings.Builder
		ctx := i18n.WithLocale(context.Background(), string(locale))
		if err := Fragment(Card{Offered: true, Session: "Push A"}).Render(ctx, &b); err != nil {
			t.Fatalf("render %s: %v", locale, err)
		}
		// templ escapes the apostrophe in the English copy.
		html := strings.ReplaceAll(b.String(), "&#39;", "'")
		for _, want := range wants {
			if !strings.Contains(html, want) {
				t.Errorf("%s: offer does not contain %q", locale, want)
			}
		}
	}
}
