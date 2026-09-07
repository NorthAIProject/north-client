package users_test

import (
	"testing"

	"github.com/NorthAIProject/north-client/internal/users"
)

// Browsers and hand-written requests send the same language four different
// ways. Nobody should lose their language to a separator.
func TestResolveLocaleNormalises(t *testing.T) {
	cases := map[string]users.Locale{
		"pt-BR": users.LocalePTBR,
		"pt_BR": users.LocalePTBR,
		"pt-br": users.LocalePTBR,
		"PT-BR": users.LocalePTBR,
		" es ":  users.LocaleES,
		"pt-PT": users.LocalePTPT,

		// A bare "pt" is ambiguous, and answering it beats refusing.
		"pt": users.LocalePTPT,

		// Anything this build does not serve reads as English.
		"":         users.LocaleEN,
		"kl-GL":    users.LocaleEN,
		"nonsense": users.LocaleEN,
	}

	for in, want := range cases {
		if got := users.ResolveLocale(in); got != want {
			t.Errorf("ResolveLocale(%q) = %q, want %q", in, got, want)
		}
	}
}

// Each language is named in itself, because someone stuck in the wrong one
// cannot read the English name to escape it.
func TestLocaleLabelsAreInTheirOwnLanguage(t *testing.T) {
	cases := map[users.Locale]string{
		users.LocaleEN:   "English",
		users.LocalePTPT: "Português (Portugal)",
		users.LocalePTBR: "Português (Brasil)",
		users.LocaleES:   "Español",
	}
	for locale, want := range cases {
		if got := locale.Label(); got != want {
			t.Errorf("%q.Label() = %q, want %q", locale, got, want)
		}
	}
}

// Every locale the settings select offers must be one Valid accepts, or the
// form would refuse to save one of its own options.
func TestEveryOfferedLocaleIsValid(t *testing.T) {
	for _, l := range users.Locales {
		if !l.Valid() {
			t.Errorf("locale %q is offered but not valid", l)
		}
		if l.Label() == "" || l.Language() == "" {
			t.Errorf("locale %q has no label or no prompt name", l)
		}
	}
}
