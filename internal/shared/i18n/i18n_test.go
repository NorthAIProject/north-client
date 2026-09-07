package i18n

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/users"
)

// The whole point of the package. A string added to English without a
// translation must fail here rather than appear in the wrong language in front
// of somebody, and a key deleted from English must not linger in three files.
func TestEveryCatalogueCoversEnglish(t *testing.T) {
	for locale, catalogue := range catalogues {
		if locale == users.LocaleEN {
			continue
		}

		var missing, extra []string
		for key := range english {
			if catalogue[key] == "" {
				missing = append(missing, key)
			}
		}
		for key := range catalogue {
			if _, ok := english[key]; !ok {
				extra = append(extra, key)
			}
		}
		sort.Strings(missing)
		sort.Strings(extra)

		if len(missing) > 0 {
			t.Errorf("%s is missing %d keys:\n  %s", locale, len(missing), strings.Join(missing, "\n  "))
		}
		if len(extra) > 0 {
			t.Errorf("%s has %d keys English does not:\n  %s", locale, len(extra), strings.Join(extra, "\n  "))
		}
	}
}

// Every locale users.Locales offers must have a catalogue, or choosing it in
// settings would silently serve English.
func TestEveryOfferedLocaleHasACatalogue(t *testing.T) {
	for _, l := range users.Locales {
		if len(catalogues[l]) == 0 {
			t.Errorf("locale %q is offered in settings but has no catalogue", l)
		}
	}
}

// A translation that is still the English is either a real cognate or an
// oversight, and there is no way to tell them apart mechanically. This lists
// them so the count is a deliberate number somebody looked at, rather than
// drifting upward unnoticed.
func TestUntranslatedStringsAreAccountedFor(t *testing.T) {
	// Genuine cognates: identical in every target language, verified rather
	// than assumed. Keep this list short — each entry is a string nobody will
	// notice going stale, so it should have to earn its place.
	allowed := map[string]bool{
		"nav.fitness":         true, // borrowed unchanged into all three
		"palette.empty.after": true, // a full stop
	}

	for locale, catalogue := range catalogues {
		if locale == users.LocaleEN {
			continue
		}
		for key, en := range english {
			if allowed[key] {
				continue
			}
			if catalogue[key] == en {
				t.Errorf("%s: %q is still the English %q — translate it, or add it to the allowed cognates with a reason",
					locale, key, en)
			}
		}
	}
}

// Nothing user-facing may fall through to the raw key. T returns the key when
// it cannot find anything, which is the visible-failure of last resort; this
// asserts it never fires for a key English actually defines.
func TestTranslateNeverReturnsAKeyForAKnownString(t *testing.T) {
	for _, l := range users.Locales {
		for key := range english {
			if got := Translate(l, key); got == key && !strings.HasSuffix(key, ".after") {
				t.Errorf("locale %q, key %q: fell through to the key", l, key)
			}
		}
	}
}

// An empty context is normal — a background job or a direct component call has
// no request behind it — and must read English rather than panic.
func TestLocaleFromEmptyContextIsEnglish(t *testing.T) {
	if got := LocaleFrom(context.Background()); got != users.LocaleEN {
		t.Errorf("LocaleFrom(empty) = %q, want %q", got, users.LocaleEN)
	}
	if got := T(context.Background(), "palette.trigger"); got != "Search" {
		t.Errorf("T(empty, palette.trigger) = %q, want the English", got)
	}
}

// A locale this build has retired reads English rather than the key.
func TestUnknownLocaleFallsBackToEnglish(t *testing.T) {
	ctx := WithLocale(context.Background(), users.Locale("kl-GL"))
	if got := T(ctx, "palette.trigger"); got != "Search" {
		t.Errorf("unknown locale: T = %q, want the English", got)
	}
}

func TestTranslationsAreServedForRealLocales(t *testing.T) {
	cases := map[users.Locale]string{
		users.LocalePTPT: "Definições",
		users.LocalePTBR: "Configurações",
		users.LocaleES:   "Ajustes",
	}
	for locale, want := range cases {
		ctx := WithLocale(context.Background(), locale)
		if got := T(ctx, "nav.settings"); got != want {
			t.Errorf("%s: nav.settings = %q, want %q", locale, got, want)
		}
	}
}
