package settings

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

func renderPill(t *testing.T, locale users.Locale) string {
	t.Helper()

	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), string(locale))
	if err := AgentPill().Render(ctx, &b); err != nil {
		t.Fatalf("render %s: %v", locale, err)
	}
	return b.String()
}

// The pill's whole job is to lead to the same place the settings card does.
// A prompt that goes somewhere else is worse than no prompt.
func TestThePillLeadsToTheConnectionsPage(t *testing.T) {
	html := renderPill(t, users.LocaleEN)

	if strings.Count(html, `href="/app/settings/connections"`) != 2 {
		t.Errorf("expected both the wide and narrow variants to point at the connections page:\n%s", html)
	}
}

func TestThePillIsTranslated(t *testing.T) {
	cases := map[users.Locale][]string{
		users.LocaleEN:   {"New", "Connect your Agent"},
		users.LocalePTPT: {"Novo", "Liga o teu agente"},
		users.LocalePTBR: {"Novo", "Conecte seu agente"},
		users.LocaleES:   {"Nuevo", "Conecta tu agente"},
	}
	for locale, wants := range cases {
		html := renderPill(t, locale)
		for _, want := range wants {
			if !strings.Contains(html, want) {
				t.Errorf("%s: pill does not contain %q", locale, want)
			}
		}
	}
}

// Below sm the label is dropped, so the icon-only variant has to carry the
// same wording as an accessible name or it announces itself as a bare link.
func TestTheNarrowPillKeepsAnAccessibleName(t *testing.T) {
	html := renderPill(t, users.LocaleES)

	if !strings.Contains(html, `aria-label="Conecta tu agente"`) {
		t.Errorf("the icon-only variant has no translated accessible name:\n%s", html)
	}
}
