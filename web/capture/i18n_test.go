package capture

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

func renderCaptureIn(t *testing.T, locale users.Locale) string {
	t.Helper()

	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), string(locale))
	user := users.User{DisplayName: "Ana", Locale: locale}
	if err := Page(user, Data{}).Render(ctx, &b); err != nil {
		t.Fatalf("render %s: %v", locale, err)
	}
	if b.Len() == 0 {
		t.Fatalf("%s rendered an empty page", locale)
	}
	return b.String()
}

func TestCaptureRendersInTheChosenLanguage(t *testing.T) {
	cases := map[users.Locale][]string{
		users.LocaleEN:   {"Quick capture", "Read it"},
		users.LocalePTPT: {"Registo rápido", "Escreve o teu dia"},
		users.LocalePTBR: {"Registro rápido", "Escreva seu dia"},
		users.LocaleES:   {"Registro rápido", "Escribe tu día"},
	}
	for locale, wants := range cases {
		html := renderCaptureIn(t, locale)
		for _, want := range wants {
			if !strings.Contains(html, want) {
				t.Errorf("%s: page does not contain %q", locale, want)
			}
		}
	}
}

// capture-recorder.js used to hold its own English. Every string it shows is
// now handed to it on the record button, so the voice path is translated too —
// including the errors, which are the only thing a person sees when the
// microphone fails.
func TestTheRecorderIsHandedItsCopy(t *testing.T) {
	html := renderCaptureIn(t, users.LocalePTPT)

	for _, want := range []string{
		`data-voice-say="Diz"`,
		`data-voice-stop="Parar"`,
		"data-voice-mic=",
		"microfone",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("expected %q on the record button", want)
		}
	}

	// The English must not also be sitting there.
	if strings.Contains(html, `data-voice-say="Say it"`) {
		t.Error("the record button still carries the English label")
	}
}
