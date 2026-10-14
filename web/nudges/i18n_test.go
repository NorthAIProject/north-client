package nudgepages

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

func TestBellRendersInTheChosenLanguage(t *testing.T) {
	cases := map[users.Locale][]string{
		users.LocaleEN:   {"Coach notes", "No nudges right now."},
		users.LocalePTPT: {"Notas do treinador", "Nenhum aviso por agora."},
		users.LocalePTBR: {"Notas do treinador", "Nenhum aviso por enquanto."},
		users.LocaleES:   {"Notas del entrenador", "Ningún aviso por ahora."},
	}
	for locale, wants := range cases {
		var b strings.Builder
		ctx := i18n.WithLocale(context.Background(), string(locale))
		if err := Bell(nil, 0).Render(ctx, &b); err != nil {
			t.Fatalf("render %s: %v", locale, err)
		}
		for _, want := range wants {
			if !strings.Contains(b.String(), want) {
				t.Errorf("%s: bell does not contain %q", locale, want)
			}
		}
	}
}

// "new" agrees in number in Spanish and Portuguese, so the count has two forms.
func TestUnreadCountAgreesInNumber(t *testing.T) {
	ctx := i18n.WithLocale(context.Background(), string(users.LocaleES))
	if got := unreadLabel(ctx, 1); got != "1 nueva" {
		t.Errorf("one = %q", got)
	}
	if got := unreadLabel(ctx, 3); got != "3 nuevas" {
		t.Errorf("many = %q", got)
	}
}
