package settings

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/connections"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

func renderSettingsIn(t *testing.T, locale users.Locale) string {
	t.Helper()

	user := users.User{DisplayName: "Ana", Email: "ana@north.test", Locale: locale}
	var b strings.Builder
	page := Page(
		user,
		SettingsSummary{Timezone: "Europe/Lisbon", Units: "metric"},
		ProfileFormFor(user),
		PreferencesForm{},
		// PushEnabled, because the device line lives inside the block that only
		// renders for a deployment that can actually send Web Push.
		NotificationsForm{PushEnabled: true, PushPublicKey: "test-key", PushDevices: 2},
		nil, nil, "", "",
	)
	ctx := i18n.WithLocale(context.Background(), string(locale))
	if err := page.Render(ctx, &b); err != nil {
		t.Fatalf("render %s: %v", locale, err)
	}
	if b.Len() == 0 {
		// templ flushes nothing on error, so an empty body is what a failure
		// deeper in the tree looks like even when Render reports success.
		t.Fatalf("%s rendered an empty page", locale)
	}
	return b.String()
}

// The page that holds the language switch has to be in the chosen language, or
// the setting is a joke: you change it and the page telling you what you
// changed is still in the language you were trying to leave.
func TestSettingsRendersInTheChosenLanguage(t *testing.T) {
	cases := map[users.Locale][]string{
		users.LocaleEN:   {"Account", "Profile", "Coaching tone", "Save profile", "Your data"},
		users.LocalePTPT: {"Conta", "Perfil", "Tom do treinador", "Guardar perfil", "Os teus dados"},
		users.LocalePTBR: {"Conta", "Perfil", "Tom do treinador", "Salvar perfil", "Seus dados"},
		users.LocaleES:   {"Cuenta", "Perfil", "Tono del entrenador", "Guardar perfil", "Tus datos"},
	}

	for locale, wants := range cases {
		html := renderSettingsIn(t, locale)
		for _, want := range wants {
			if !strings.Contains(html, want) {
				t.Errorf("%s: page does not contain %q", locale, want)
			}
		}
	}
}

// The specific regression this guards: a Portuguese or Spanish page still
// showing English headings because a string was missed in the conversion.
func TestNoEnglishHeadingsLeakIntoATranslatedPage(t *testing.T) {
	// Headings, not prose: a false positive on a word that happens to be
	// spelled the same in both languages would make this test useless.
	englishOnly := []string{
		">Account<",
		">Profile<",
		"Coaching tone",
		"Save profile",
		"Fitness defaults",
		"Dietary preferences",
		"Your data",
		"Delete my account",
	}

	for _, locale := range []users.Locale{users.LocalePTPT, users.LocalePTBR, users.LocaleES} {
		html := renderSettingsIn(t, locale)
		for _, leak := range englishOnly {
			if strings.Contains(html, leak) {
				t.Errorf("%s: English %q is still on the page", locale, leak)
			}
		}
	}
}

// Two devices, not one, so the plural branch is the one exercised — the
// singular is not "1 devices" in any of these languages.
func TestThePluralDeviceLineIsTranslated(t *testing.T) {
	cases := map[users.Locale]string{
		users.LocaleEN:   "2 devices get nudges.",
		users.LocalePTPT: "2 dispositivos recebem avisos.",
		users.LocaleES:   "2 dispositivos reciben avisos.",
	}
	for locale, want := range cases {
		if html := renderSettingsIn(t, locale); !strings.Contains(html, want) {
			t.Errorf("%s: expected %q", locale, want)
		}
	}
}

// The tone select renders users.Tone through the catalogue, not through a
// hardcoded English label.
func TestToneOptionsAreTranslated(t *testing.T) {
	html := renderSettingsIn(t, users.LocaleES)
	for _, want := range []string{"Directo", "Cercano", "Analítico", "Exigente"} {
		if !strings.Contains(html, want) {
			t.Errorf("Spanish tone %q is missing", want)
		}
	}
}

// The page the topbar pill leads to. Somebody who follows a Portuguese prompt
// should not arrive at an English page.
func TestConnectionsRendersInTheChosenLanguage(t *testing.T) {
	cases := map[users.Locale][]string{
		users.LocaleEN:   {"Agent connections", "Connected agents", "Connect an agent"},
		users.LocalePTPT: {"Ligações de agentes", "Agentes ligados", "Ligar um agente"},
		users.LocalePTBR: {"Conexões de agentes", "Agentes conectados", "Conectar um agente"},
		users.LocaleES:   {"Conexiones de agentes", "Agentes conectados", "Conectar un agente"},
	}

	for locale, wants := range cases {
		ctx := i18n.WithLocale(context.Background(), string(locale))
		var b strings.Builder
		page := ConnectionsPage(
			users.User{DisplayName: "Ana", Locale: locale},
			nil, ConnectForm{}, nil, connections.Setup{}, nil, ProviderPanel{},
			TelegramPanel{}, CalendarPanel{}, "https://north.example.com/mcp",
		)
		if err := page.Render(ctx, &b); err != nil {
			t.Fatalf("render %s: %v", locale, err)
		}
		html := b.String()
		for _, want := range wants {
			if !strings.Contains(html, want) {
				t.Errorf("%s: page does not contain %q", locale, want)
			}
		}
	}
}
