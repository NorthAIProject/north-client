package auth

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

func renderLoginIn(t *testing.T, locale string) string {
	t.Helper()

	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), locale)
	form := LoginForm{Options: AuthOptions{GoogleEnabled: true, PasskeyEnabled: true, PasswordResetEnabled: true}}
	if err := LoginPage(form).Render(ctx, &b); err != nil {
		t.Fatalf("render %s: %v", locale, err)
	}
	if b.Len() == 0 {
		t.Fatalf("%s rendered an empty page", locale)
	}
	return b.String()
}

// The sign-in page is where somebody arriving from a Spanish landing page
// first types anything. It has to be in the language they came in with.
func TestLoginRendersInTheChosenLanguage(t *testing.T) {
	cases := map[string][]string{
		"en":    {"Welcome back", "Forgot password?", "Continue with Google", "Create one"},
		"es":    {"Hola de nuevo", "¿Olvidaste la contraseña?", "Continuar con Google", "Crea una"},
		"pt-PT": {"Bem-vindo de volta", "Palavra-passe", "Esqueceste a palavra-passe?", "Cria uma"},
		"pt-BR": {"Que bom te ver de novo", "Senha", "Esqueceu a senha?", "Crie uma"},
	}
	for locale, wants := range cases {
		html := renderLoginIn(t, locale)
		for _, want := range wants {
			if !strings.Contains(html, want) {
				t.Errorf("%s: page does not contain %q", locale, want)
			}
		}
	}
}

func TestNoEnglishLeaksIntoATranslatedLogin(t *testing.T) {
	englishOnly := []string{"Welcome back", "Forgot password?", "Use a passkey", "No account?", "Continue your thread"}
	for _, locale := range []string{"es", "pt-PT", "pt-BR"} {
		html := renderLoginIn(t, locale)
		for _, leak := range englishOnly {
			if strings.Contains(html, leak) {
				t.Errorf("%s: English %q is still on the page", locale, leak)
			}
		}
	}
}

// The consent notice is the one place signup asks for something with legal
// weight, so it must not quietly stay English.
func TestSignupConsentIsTranslated(t *testing.T) {
	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), "es")
	if err := SignupPage(SignupForm{}).Render(ctx, &b); err != nil {
		t.Fatalf("render: %v", err)
	}
	html := b.String()
	for _, want := range []string{"Crea tu cuenta", "política de privacidad", "consentimiento expreso"} {
		if !strings.Contains(html, want) {
			t.Errorf("signup does not contain %q", want)
		}
	}
	if strings.Contains(html, "explicitly consent") {
		t.Error("the English consent notice is still on the Spanish page")
	}
}
