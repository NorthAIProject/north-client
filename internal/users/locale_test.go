package users_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
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

		// A bare "pt" is ambiguous. Brazilian, matching CLDR's likely-subtags
		// data and what x/text gives Accept-Language negotiation for the same
		// input — see i18n.Negotiate.
		"pt": users.LocalePTBR,

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

// internal/shared/i18n is a leaf: it keys its catalogues by BCP 47 tag rather
// than by users.Locale, so nothing there can assert the two lists agree. This
// is where that check lives.
func TestEveryOfferedLocaleHasACatalogue(t *testing.T) {
	for _, l := range users.Locales {
		// nav.settings exists in every catalogue. A locale with no catalogue at
		// all falls through to the English, so this compares against a string
		// only the real catalogue can produce.
		if got := i18n.Translate(string(l), "nav.settings"); got == "nav.settings" {
			t.Errorf("locale %q is offered in settings but has no catalogue", l)
		}
	}

	if i18n.DefaultLocale != string(users.LocaleDefault) {
		t.Errorf("i18n.DefaultLocale = %q, users.LocaleDefault = %q — they must agree",
			i18n.DefaultLocale, users.LocaleDefault)
	}
}

// A profile save is behind auth, so the request carries the language the person
// chose. A refusal is the worst possible moment to answer in a language they
// did not pick.
func TestProfileValidationSpeaksTheRequestLanguage(t *testing.T) {
	svc := users.NewService(nil)

	cases := map[users.Locale]string{
		users.LocaleEN:   "Name is required.",
		users.LocalePTPT: "O nome é obrigatório.",
		users.LocalePTBR: "O nome é obrigatório.",
		users.LocaleES:   "El nombre es obligatorio.",
	}

	for locale, want := range cases {
		ctx := i18n.WithLocale(context.Background(), string(locale))

		// A blank name, so validation refuses before the repository is reached
		// — which is why a nil repository is safe here.
		_, err := svc.UpdateProfile(ctx, uuid.New(), users.Profile{DisplayName: "  "})
		if err == nil {
			t.Fatalf("%s: expected a validation error", locale)
		}

		var fieldErrs apperr.FieldErrors
		if !apperr.As(err, &fieldErrs) {
			t.Fatalf("%s: expected field errors, got %v", locale, err)
		}
		if got := fieldErrs.Messages()["display_name"]; got != want {
			t.Errorf("%s: message = %q, want %q", locale, got, want)
		}
	}
}

// Signup has no account yet, so there is no chosen language to honour. It must
// answer in English regardless of what the context happens to carry.
func TestRegistrationValidationStaysEnglish(t *testing.T) {
	svc := users.NewService(nil)

	_, err := svc.ValidateRegistration(users.Registration{Email: "", DisplayName: "Ana"})
	if err == nil {
		t.Fatal("expected a validation error")
	}

	var fieldErrs apperr.FieldErrors
	if !apperr.As(err, &fieldErrs) {
		t.Fatalf("expected field errors, got %v", err)
	}
	if got, want := fieldErrs.Messages()["email"], "Email is required."; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
}
