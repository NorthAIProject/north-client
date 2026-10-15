package day

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

// BMI is an English acronym; the Romance languages say IMC.
func TestBMIChipIsTranslated(t *testing.T) {
	card := BodyCard{HasBMI: true, BMI: "22.1", BMICategory: "healthy"}
	cases := map[users.Locale]string{
		users.LocaleEN:   "BMI",
		users.LocalePTPT: "IMC",
		users.LocalePTBR: "IMC",
		users.LocaleES:   "IMC",
	}
	for locale, want := range cases {
		ctx := i18n.WithLocale(context.Background(), string(locale))
		var b strings.Builder
		if err := bodyChips(card).Render(ctx, &b); err != nil {
			t.Fatalf("render %s: %v", locale, err)
		}
		html := b.String()
		if !strings.Contains(html, want) {
			t.Errorf("%s: chip does not contain %q", locale, want)
		}
		if locale != users.LocaleEN && strings.Contains(html, "BMI") {
			t.Errorf("%s: English BMI is still on the chip", locale)
		}
	}
}
