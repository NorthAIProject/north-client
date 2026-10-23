package awards_test

import (
	"context"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/migrations"
)

// The migration tells each existing account once, in its own language, that
// awards now show to followers; Down removes the note again.
func TestMigrationNoticeSpeaksEachLanguageOnce(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	src, err := migrations.FS.ReadFile("20261023100000_awards.sql")
	if err != nil {
		t.Fatal(err)
	}
	up, down, _ := strings.Cut(string(src), "-- +goose Down")
	start := strings.Index(up, "INSERT INTO user_nudges")
	if start < 0 {
		t.Fatal("the awards migration inserts no notice")
	}
	insert := up[start : start+strings.Index(up[start:], ";")+1]
	if !strings.Contains(down, "DELETE FROM user_nudges WHERE kind = 'awards_sharing'") {
		t.Fatal("Down does not remove the notice")
	}
	want := map[string]string{"en": "awards", "es": "premios", "pt-PT": "prémios", "pt-BR": "prêmios"}
	ids := map[string]string{}
	for loc := range want {
		u := newUser(t, pool, "mig-"+loc+"@example.com", "UTC")
		ids[loc] = u.String()
		if _, err := pool.Exec(ctx, `UPDATE users SET locale = $2 WHERE id = $1`, u, loc); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `DELETE FROM user_nudges WHERE kind = 'awards_sharing'`); err != nil {
		t.Fatal(err)
	}
	for range 2 { // a re-run must not repeat it
		if _, err := pool.Exec(ctx, insert); err != nil {
			t.Fatal(err)
		}
	}
	for loc, word := range want {
		var n int
		var title, body, href string
		if err := pool.QueryRow(ctx, `SELECT count(*) OVER (), title, body, href FROM user_nudges WHERE user_id = $1 AND kind = 'awards_sharing'`, ids[loc]).Scan(&n, &title, &body, &href); err != nil {
			t.Fatalf("%s: %v", loc, err)
		}
		if n != 1 || !strings.Contains(strings.ToLower(title+body), word) || href != "/app/friends#sharing" {
			t.Errorf("%s: %d notes, %q / %q -> %q", loc, n, title, body, href)
		}
	}
}
