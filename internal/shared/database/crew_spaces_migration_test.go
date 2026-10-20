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
	beforeCrewSpaces = 20261016140000 // the migration just before crew spaces
	crewSpaces       = 20261020120000 // crews get channels, roles and visibility
)

// Every crew that existed before spaces gets #general and #wins, stays
// invite-only, and its owner is not turned into an admin row: ownership
// stays in crews.owner_id. Down and Up again must not duplicate channels.
func TestCrewSpacesMigrationBackfillsChannels(t *testing.T) {
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

	const dbName = "north_crew_spaces_check"
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

	if _, err = provider.UpTo(ctx, beforeCrewSpaces); err != nil {
		t.Fatalf("migrate to %d: %v", beforeCrewSpaces, err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, execErr := db.ExecContext(ctx, query, args...); execErr != nil {
			t.Fatalf("%s: %v", query, execErr)
		}
	}
	const (
		owner = "00000000-0000-0000-0000-000000000001"
		crew  = "00000000-0000-0000-0000-0000000000c1"
	)
	exec(`INSERT INTO users (id, email, password_hash, display_name, timezone)
		VALUES ($1, 'o@north.test', 'x', 'O', 'UTC')`, owner)
	exec(`INSERT INTO crews (id, name, owner_id, code) VALUES ($1, 'Iron rats', $2, 'abcdefghjk')`, crew, owner)
	exec(`INSERT INTO crew_members (crew_id, user_id) VALUES ($1, $2)`, crew, owner)

	if _, err = provider.UpTo(ctx, crewSpaces); err != nil {
		t.Fatalf("migrate to %d: %v", crewSpaces, err)
	}

	type channel struct {
		kind, name string
		isDefault  bool
		position   int
	}
	channels := func() []channel {
		t.Helper()
		rows, qErr := db.QueryContext(ctx, `SELECT kind, name, is_default, position FROM crew_channels
			WHERE crew_id = $1 ORDER BY position`, crew)
		if qErr != nil {
			t.Fatalf("list channels: %v", qErr)
		}
		defer func() { _ = rows.Close() }()
		var out []channel
		for rows.Next() {
			var c channel
			if qErr = rows.Scan(&c.kind, &c.name, &c.isDefault, &c.position); qErr != nil {
				t.Fatalf("scan channel: %v", qErr)
			}
			out = append(out, c)
		}
		return out
	}
	want := []channel{{"text", "general", true, 0}, {"wins", "wins", false, 1}}
	got := channels()
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("channels = %+v; want %+v", got, want)
	}

	var visibility, role string
	if err = db.QueryRowContext(ctx, `SELECT visibility FROM crews WHERE id = $1`, crew).Scan(&visibility); err != nil {
		t.Fatalf("read visibility: %v", err)
	}
	if err = db.QueryRowContext(ctx, `SELECT role FROM crew_members WHERE crew_id = $1 AND user_id = $2`, crew, owner).Scan(&role); err != nil {
		t.Fatalf("read role: %v", err)
	}
	if visibility != "invite" || role != "member" {
		t.Errorf("visibility %q, owner role %q; want invite and member", visibility, role)
	}

	if _, err = provider.DownTo(ctx, beforeCrewSpaces); err != nil {
		t.Fatalf("migrate down to %d: %v", beforeCrewSpaces, err)
	}
	if _, err = provider.UpTo(ctx, crewSpaces); err != nil {
		t.Fatalf("migrate up again: %v", err)
	}
	if got = channels(); len(got) != 2 {
		t.Errorf("after down and up, %d channels; want 2", len(got))
	}
}
