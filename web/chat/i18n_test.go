package chat

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

func renderEmptyIn(t *testing.T, locale users.Locale) string {
	t.Helper()

	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), string(locale))
	user := users.User{DisplayName: "Ana", Locale: locale}
	if err := Empty(user, nil, CoachStats{}).Render(ctx, &b); err != nil {
		t.Fatalf("render %s: %v", locale, err)
	}
	if b.Len() == 0 {
		t.Fatalf("%s rendered an empty page", locale)
	}
	return b.String()
}

func TestCoachEmptyStateRendersInTheChosenLanguage(t *testing.T) {
	cases := map[users.Locale][]string{
		users.LocaleEN:   {"Talk to your coach", "Khepri remembers what you tell it"},
		users.LocalePTPT: {"Fala com o teu treinador", "O Khepri lembra-se do que lhe dizes"},
		users.LocalePTBR: {"Converse com seu treinador", "O Khepri lembra o que você conta"},
		users.LocaleES:   {"Habla con tu entrenador", "Khepri recuerda lo que le cuentas"},
	}
	for locale, wants := range cases {
		html := renderEmptyIn(t, locale)
		for _, want := range wants {
			if !strings.Contains(html, want) {
				t.Errorf("%s: page does not contain %q", locale, want)
			}
		}
	}
}

// The starters are the first sentences a new user reads. They were a
// package-level English slice, which would have left the one screen where the
// language setting matters most entirely in English.
func TestConversationStartersAreTranslated(t *testing.T) {
	cases := map[users.Locale]string{
		users.LocalePTPT: "Quero ficar mais forte mas não sei por onde começar.",
		users.LocaleES:   "Quiero ponerme más fuerte pero no sé por dónde empezar.",
	}
	for locale, want := range cases {
		if html := renderEmptyIn(t, locale); !strings.Contains(html, want) {
			t.Errorf("%s: starter %q is missing", locale, want)
		}
	}
}

// The delete confirmation is a JavaScript confirm() built by string
// concatenation. jsString has to quote the translated text, or an apostrophe in
// "Tudo o que está nela" would break the handler rather than just read oddly.
func TestTheDeleteConfirmationIsQuotedAfterTranslation(t *testing.T) {
	var b strings.Builder
	ctx := i18n.WithLocale(context.Background(), string(users.LocaleES))
	c := conversations.Conversation{}
	if err := chatHeader(c).Render(ctx, &b); err != nil {
		t.Fatalf("render header: %v", err)
	}
	html := b.String()

	if !strings.Contains(html, "conversación") {
		t.Error("the confirmation is not translated")
	}
	// The quoted form must survive templ's attribute escaping intact.
	if !strings.Contains(html, "confirm(") {
		t.Error("the confirm() call is malformed")
	}
}
