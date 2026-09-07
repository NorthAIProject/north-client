// Package i18n holds the translated copy for every user-facing surface, and the
// lookup that picks the right one for a request.
//
// # Why maps in Go rather than files
//
// The catalogues are Go maps, not embedded JSON or TOML. Three reasons, in
// order: a missing key is caught by a test in this package rather than at
// render time in front of a user; `grep` finds a string and the code that uses
// it in one search; and there is no parser, no schema and no new dependency for
// something that is fundamentally a constant table.
//
// The cost is that a translator needs a pull request rather than a text editor.
// That is the right trade at four languages maintained by the people who wrote
// the English. It stops being the right trade the moment a translator who does
// not write Go is doing the work, and at that point these files become the
// thing a generator writes.
//
// # Why English is the key
//
// Keys are dotted identifiers ("settings.profile.title"), not English strings.
// English-as-key reads beautifully until the English changes: every catalogue
// silently falls back at once, with no compile error and no test failure,
// because the key that changed still exists as a valid string. An identifier
// that nobody is tempted to edit for style avoids that entirely.
package i18n

import (
	"context"
	"fmt"

	"github.com/NorthAIProject/north-client/internal/users"
)

// catalogues holds every locale this build serves. English is the reference:
// TestEveryCatalogueCoversEnglish asserts the others match its key set, so a
// string added in English cannot ship untranslated without a red test.
var catalogues = map[users.Locale]map[string]string{
	users.LocaleEN:   english,
	users.LocalePTPT: portugueseEuropean,
	users.LocalePTBR: portugueseBrazilian,
	users.LocaleES:   spanish,
}

// Catalogues are written one surface at a time and merged here, rather than as
// one map per language. Four files of a thousand entries would be unreviewable
// and every translation would collide in the same lines of every diff; one file
// per surface per language keeps a change to the settings page a change to four
// small files.
var (
	english             = merge(englishNav, englishSettings)
	portugueseEuropean  = merge(portugueseEuropeanNav, portugueseEuropeanSettings)
	portugueseBrazilian = merge(portugueseBrazilianNav, portugueseBrazilianSettings)
	spanish             = merge(spanishNav, spanishSettings)
)

// merge folds the per-surface maps into one catalogue, and panics on a
// duplicate key.
//
// Panicking at init is right here: a key defined twice means one surface is
// silently overriding another's copy, which is invisible in review and would
// surface as the wrong sentence on a page nobody was editing. Every caller of
// this is a package-level var, so the panic happens at startup in every
// environment including the test binary — it cannot reach a user.
func merge(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			if _, clash := out[k]; clash {
				panic("i18n: duplicate catalogue key " + k)
			}
			out[k] = v
		}
	}
	return out
}

type localeKey struct{}

// WithLocale carries the locale for this request. Set once by middleware; every
// template reads it from the context templ already threads through.
func WithLocale(ctx context.Context, l users.Locale) context.Context {
	return context.WithValue(ctx, localeKey{}, l)
}

// LocaleFrom returns the request's locale, English when nothing set one.
//
// A missing locale is normal rather than exceptional: a background job
// rendering a template, or a test calling a component directly, has no request
// behind it. Those should read English, not panic.
func LocaleFrom(ctx context.Context) users.Locale {
	if l, ok := ctx.Value(localeKey{}).(users.Locale); ok && l.Valid() {
		return l
	}
	return users.LocaleDefault
}

// T returns the translation of key for the request's locale.
//
// Falling back to English rather than showing the key is deliberate. A user who
// meets an untranslated string reads a sentence in the wrong language, which is
// mildly annoying; a user who meets "settings.profile.title" reads a bug. The
// test in this package is what stops the fallback becoming the normal case.
func T(ctx context.Context, key string) string {
	return Translate(LocaleFrom(ctx), key)
}

// Tf is T with formatting, for the handful of strings carrying a count or a
// name. Argument order can differ between languages, so every such string uses
// explicit indexes ("%[1]s") rather than bare verbs.
func Tf(ctx context.Context, key string, args ...any) string {
	return fmt.Sprintf(Translate(LocaleFrom(ctx), key), args...)
}

// Translate looks a key up in one locale, without a request. Exported for the
// worker, which renders nudge and briefing copy for a user it loaded rather
// than one who is calling.
func Translate(l users.Locale, key string) string {
	if s, ok := catalogues[l][key]; ok && s != "" {
		return s
	}
	if s, ok := english[key]; ok {
		return s
	}
	// Nothing to say and nothing to fall back to. The key is the least bad
	// thing to render: it is visibly wrong, which is what gets it fixed.
	return key
}

// Keys returns every key the English reference defines, sorted by the caller if
// it cares. Used by the parity test.
func Keys() []string {
	out := make([]string, 0, len(english))
	for k := range english {
		out = append(out, k)
	}
	return out
}
