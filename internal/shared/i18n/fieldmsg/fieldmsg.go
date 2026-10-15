// Package fieldmsg says a service's field errors in the reader's language.
//
// # Why services keep writing English
//
// The same service methods answer a person filling in a form and the coach
// calling a tool on their behalf. The coach must read English — its prompts,
// tools and context all are — so a service cannot translate its own messages
// from the request's locale without translating them for the coach as well.
//
// Instead a service writes English, exactly as before, and the handler that
// shows the error to a person passes it through Localize with a table naming
// the catalogue key for each message. The coach's path never calls Localize,
// so it reads what it always read.
//
// A message the table does not know stays English, which is the fallback T
// gives for any missing translation. Mismatches is what stops that being the
// normal case: each package tests that its table's English is what its
// catalogue says, so editing one without the other fails a test.
package fieldmsg

import (
	"context"
	"fmt"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// Message pairs a field message a service writes with the catalogue key that
// says it. Args fill the key's verbs, for a message carrying a fixed number
// such as a length limit.
//
// Match is for the rare message that carries a value only known at the time,
// such as a person's own target: it reports whether msg is this message, and
// the arguments to fill the key with. English is ignored when Match is set.
type Message struct {
	English string
	Key     string
	Args    []any
	Match   func(msg string) (args []any, ok bool)
}

// Localize returns err with each field message the table knows said in ctx's
// language. Anything that is not a field error is returned as it was.
func Localize(ctx context.Context, err error, table []Message) error {
	var fields apperr.FieldErrors
	if err == nil || !apperr.As(err, &fields) || i18n.LocaleFrom(ctx) == i18n.DefaultLocale {
		return err
	}
	out := make(apperr.FieldErrors, len(fields))
	for i, f := range fields {
		if key, args, ok := lookup(table, f.Message); ok {
			f.Message = i18n.Tf(ctx, key, args...)
		}
		out[i] = f
	}
	return out
}

func lookup(table []Message, msg string) (string, []any, bool) {
	for _, m := range table {
		if m.Match != nil {
			if args, ok := m.Match(msg); ok {
				return m.Key, args, true
			}
			continue
		}
		if m.English == msg {
			return m.Key, m.Args, true
		}
	}
	return "", nil, false
}

// English is a key's English with args filled in: what a service writes when
// it builds a message from the catalogue rather than a literal.
func English(key string, args ...any) string {
	return fmt.Sprintf(i18n.Translate(i18n.DefaultLocale, key), args...)
}

// Mismatches lists every entry whose English is not what its key says in the
// English catalogue. Entries with Match are skipped; their own tests cover them.
func Mismatches(table []Message) []string {
	var out []string
	for _, m := range table {
		if m.Match != nil {
			continue
		}
		if got := English(m.Key, m.Args...); got != m.English {
			out = append(out, fmt.Sprintf("%s: catalogue says %q, service writes %q", m.Key, got, m.English))
		}
	}
	return out
}
