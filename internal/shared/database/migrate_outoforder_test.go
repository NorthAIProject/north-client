package database_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/NorthAIProject/north-client/internal/shared/database"
)

// The failure this guards against: two branches developed in parallel each
// add a migration, and whichever merges second carries a version lower than
// one the database has already applied. Without WithAllowOutofOrder that is
// not a warning — the application refuses to boot, and every developer with
// the other branch's database is stuck until someone renumbers files by hand.
//
// Simulated by marking a version as applied that is higher than one of the
// real migrations, then asking Migrate to run against it. Before the option
// was set this returned "missing (out-of-order) migration".
func TestMigrateAppliesOutOfOrderMigrations(t *testing.T) {
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

	const dbName = "north_ooo_check"
	if _, err = admin.ExecContext(ctx, `DROP DATABASE IF EXISTS `+dbName+` WITH (FORCE)`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err = admin.ExecContext(ctx, `CREATE DATABASE `+dbName); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), `DROP DATABASE IF EXISTS `+dbName+` WITH (FORCE)`)
	})

	targetURL := swapDatabase(url, dbName)

	// A full, honest run first.
	if err = database.Migrate(ctx, targetURL); err != nil {
		t.Fatalf("initial migrate: %v", err)
	}

	// Now stage the conflict: forget one applied migration from the middle,
	// exactly as a database migrated from a branch that never had it would
	// look. The next boot sees a version below its maximum going missing.
	target, err := sql.Open("pgx", targetURL)
	if err != nil {
		t.Fatalf("open target: %v", err)
	}
	defer func() { _ = target.Close() }()

	if _, err := target.ExecContext(ctx, `DELETE FROM goose_db_version WHERE version_id = 21`); err != nil {
		t.Fatalf("stage missing migration: %v", err)
	}
	if _, err := target.ExecContext(ctx, `DROP TABLE IF EXISTS habit_completions, habits`); err != nil {
		t.Fatalf("drop staged tables: %v", err)
	}

	// The real assertion: this boots rather than refusing.
	if err := database.Migrate(ctx, targetURL); err != nil {
		t.Fatalf("migrate with an out-of-order migration should succeed, got: %v", err)
	}

	// And it actually applied the missing one rather than merely tolerating it.
	var count int
	if err := target.QueryRowContext(ctx,
		`SELECT count(*) FROM information_schema.tables WHERE table_name = 'habits'`,
	).Scan(&count); err != nil {
		t.Fatalf("check table: %v", err)
	}
	if count != 1 {
		t.Error("the out-of-order migration was skipped rather than applied")
	}
}

// swapDatabase rewrites the database name in a postgres URL, keeping
// credentials and query parameters intact.
func swapDatabase(url, name string) string {
	base, query, hasQuery := strings.Cut(url, "?")

	slash := strings.LastIndex(base, "/")
	if slash < 0 {
		return url
	}
	out := base[:slash+1] + name

	if hasQuery {
		return out + "?" + query
	}
	return out
}
