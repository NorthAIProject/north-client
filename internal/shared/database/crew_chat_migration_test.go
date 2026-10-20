package database_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/NorthAIProject/north-client/migrations"
)

const (
	beforeCrewChat = 20261020130000 // moderation, the migration just before chat
	crewChat       = 20261021090000 // crew messages, reactions and read cursors
)

// Every crew that existed before chat gets one system message in its default
// channel telling members the crew now has channels. Down and Up again must
// leave exactly one.
func TestCrewChatMigrationBackfillsSystemMessage(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		t.Skip("no TEST_DATABASE_URL or DATABASE_URL set; skipping database test")
	}
	ctx := context.Background()

	admin, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open admin connection: %v", err)
	}
	defer func() { _ = admin.Close() }()

	const dbName = "north_crew_chat_check"
	if _, err = admin.ExecContext(ctx, `DROP DATABASE IF EXISTS `+dbName+` WITH (FORCE)`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err = admin.ExecContext(ctx, `CREATE DATABASE `+dbName); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), `DROP DATABASE IF EXISTS `+dbName+` WITH (FORCE)`)
	})

	db, err := sql.Open("pgx", swapDatabase(url, dbName))
	if err != nil {
		t.Fatalf("open target: %v", err)
	}
	defer func() { _ = db.Close() }()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS, goose.WithAllowOutofOrder(true))
	if err != nil {
		t.Fatalf("migration provider: %v", err)
	}

	if _, err = provider.UpTo(ctx, beforeCrewChat); err != nil {
		t.Fatalf("migrate to %d: %v", beforeCrewChat, err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, execErr := db.ExecContext(ctx, query, args...); execErr != nil {
			t.Fatalf("%s: %v", query, execErr)
		}
	}
	const (
		owner   = "00000000-0000-0000-0000-000000000001"
		crew    = "00000000-0000-0000-0000-0000000000c1"
		general = "00000000-0000-0000-0000-0000000000a1"
	)
	exec(`INSERT INTO users (id, email, password_hash, display_name, timezone)
		VALUES ($1, 'o@north.test', 'x', 'O', 'UTC')`, owner)
	exec(`INSERT INTO crews (id, name, owner_id, code) VALUES ($1, 'Iron rats', $2, 'abcdefghjk')`, crew, owner)
	exec(`INSERT INTO crew_members (crew_id, user_id) VALUES ($1, $2)`, crew, owner)
	exec(`INSERT INTO crew_channels (id, crew_id, kind, name, is_default) VALUES ($1, $2, 'text', 'general', true)`, general, crew)
	exec(`INSERT INTO crew_channels (crew_id, kind, name, position) VALUES ($1, 'wins', 'wins', 1)`, crew)

	if _, err = provider.UpTo(ctx, crewChat); err != nil {
		t.Fatalf("migrate to %d: %v", crewChat, err)
	}

	check := func() {
		t.Helper()
		rows, qErr := db.QueryContext(ctx, `SELECT kind, author_id IS NULL, body, channel_id::text
			FROM crew_messages WHERE crew_id = $1`, crew)
		if qErr != nil {
			t.Fatalf("list messages: %v", qErr)
		}
		defer func() { _ = rows.Close() }()
		n := 0
		for rows.Next() {
			var kind, body, channel string
			var system bool
			if qErr = rows.Scan(&kind, &system, &body, &channel); qErr != nil {
				t.Fatalf("scan: %v", qErr)
			}
			n++
			if kind != "system" || !system || body != "crews.sys.spaces" || channel != general {
				t.Errorf("message = %s/%v/%q in %s; want a system crews.sys.spaces in #general", kind, system, body, channel)
			}
		}
		if n != 1 {
			t.Fatalf("%d messages; want 1", n)
		}
	}
	check()

	if _, err = provider.DownTo(ctx, beforeCrewChat); err != nil {
		t.Fatalf("migrate down to %d: %v", beforeCrewChat, err)
	}
	if _, err = provider.UpTo(ctx, crewChat); err != nil {
		t.Fatalf("migrate up again: %v", err)
	}
	check()
}
