package nudges_test

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
)

// The sweep writes a nudge in the language of the account it is for, read
// from the account it already loaded.
func TestEvaluateWritesInTheRecipientsLanguage(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)

	cases := map[string]struct{ title, body string }{
		"es":    {"Tómate un momento para ti", "Han pasado 3 días desde tu último registro."},
		"pt-PT": {"Tira um momento para ti", "Passaram 3 dias desde o teu último registo."},
	}
	for locale, want := range cases {
		user := seedUser(t, pool, "eval-locale-"+locale+"@north.test")
		if _, err := pool.Exec(ctx, `UPDATE users SET locale = $2 WHERE id = $1`, user.ID, locale); err != nil {
			t.Fatal(err)
		}
		user = mustOnboard(t, pool, user, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
		writeCheckIn(t, pool, user.ID, time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC))

		svc := evalService(pool, now)
		if n, err := svc.Evaluate(ctx, user); err != nil || n != 1 {
			t.Fatalf("%s: created %d, %v; want 1", locale, n, err)
		}
		list, err := svc.ListOpen(ctx, user.ID, 20)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 || list[0].Title != want.title || list[0].Body != want.body {
			t.Fatalf("%s: open = %#v, want %q / %q", locale, list, want.title, want.body)
		}
	}
}
