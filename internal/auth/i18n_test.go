package auth

import (
	"context"
	"strings"
	"testing"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

func TestPasswordErrorsReadTheRequestLanguage(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":      "Use at least 12 characters. A short sentence works well.",
		"es":    "Usa al menos 12 caracteres. Una frase corta funciona bien.",
		"pt-BR": "Use pelo menos 12 caracteres. Uma frase curta funciona bem.",
	}
	for locale, want := range cases {
		ctx := context.Background()
		if locale != "" {
			ctx = i18n.WithLocale(ctx, locale)
		}
		var fields apperr.FieldErrors
		if !apperr.As(ValidatePassword(ctx, "short"), &fields) {
			t.Fatalf("%q: expected field errors", locale)
		}
		if got := fields.Messages()["password"]; got != want {
			t.Errorf("%q: password error = %q, want %q", locale, got, want)
		}
	}
}

func TestPasswordResetEmailIsInTheAccountLanguage(t *testing.T) {
	t.Parallel()

	const link = "https://khepri.example/reset-password?token=abc"
	cases := []struct {
		tag, subject, greeting, phrase string
	}{
		{"en", "Reset your Khepri password", "Hi Ana,", "it expires in one hour"},
		{"es", "Restablece tu contraseña de Khepri", "Hola, Ana:", "caduca en una hora"},
		{"pt-BR", "Redefina sua senha do Khepri", "Olá, Ana,", "Sua senha continua a mesma."},
		// An account with no locale yet reads English rather than a key.
		{"", "Reset your Khepri password", "Hi Ana,", "it expires in one hour"},
	}
	for _, tc := range cases {
		msg := passwordResetEmail(tc.tag, "Ana", link)
		if msg.Subject != tc.subject {
			t.Errorf("%q: subject = %q, want %q", tc.tag, msg.Subject, tc.subject)
		}
		if !strings.HasPrefix(msg.Body, tc.greeting) {
			t.Errorf("%q: body starts %q, want greeting %q", tc.tag, firstLine(msg.Body), tc.greeting)
		}
		for _, want := range []string{tc.phrase, link, "— Khepri"} {
			if !strings.Contains(msg.Body, want) {
				t.Errorf("%q: body is missing %q:\n%s", tc.tag, want, msg.Body)
			}
		}
	}
}

func TestPasswordResetEmailWithoutANameGreetsPlainly(t *testing.T) {
	t.Parallel()

	if got := passwordResetEmail("es", "", "https://x").Body; !strings.HasPrefix(got, "Hola:") {
		t.Errorf("body starts %q, want a bare greeting", firstLine(got))
	}
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
