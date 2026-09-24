package capture

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/voice"
)

func renderCaptureIn(t *testing.T, locale users.Locale) string {
	t.Helper()
	return renderCapture(t, i18n.WithLocale(voice.WithDictation(context.Background(), true), string(locale)), locale)
}

func renderCapture(t *testing.T, ctx context.Context, locale users.Locale) string {
	t.Helper()

	var b strings.Builder
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

// The recorder holds no English of its own. Every string it shows is handed to
// it on the microphone button, so the voice path is translated too — including
// the errors, which are the only thing a person sees when the microphone fails.
func TestTheRecorderIsHandedItsCopy(t *testing.T) {
	html := renderCaptureIn(t, users.LocalePTPT)

	for _, want := range []string{
		`data-dictate-say="Diz"`,
		`data-dictate-stop="Parar"`,
		"data-dictate-mic=",
		"microfone",
		// The box has a parse behind it, so it is metered as a voice note
		// rather than as dictation.
		`data-dictate-surface="capture"`,
		`data-dictate-target="capture-text"`,
		`id="capture-text"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("expected %q on the page", want)
		}
	}

	// The English must not also be sitting there.
	if strings.Contains(html, `data-dictate-say="Say it"`) {
		t.Error("the record button still carries the English label")
	}
}

// A deployment that cannot transcribe offers the typed box and nothing else: no
// button, and no script to load for it.
func TestNoMicrophoneWhereNothingCanListen(t *testing.T) {
	ctx := i18n.WithLocale(voice.WithDictation(context.Background(), false), string(users.LocaleEN))
	html := renderCapture(t, ctx, users.LocaleEN)

	for _, unwanted := range []string{"data-dictate", "dictate.js"} {
		if strings.Contains(html, unwanted) {
			t.Errorf("page contains %q with dictation off", unwanted)
		}
	}
}
