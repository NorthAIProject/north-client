// Package testdb creates disposable, fully migrated databases for tests.
//
// Tests run against real Postgres rather than a mock. North's persistence uses
// citext, inet, jsonb, ON DELETE CASCADE, and unique constraints for
// correctness under concurrency; a fake repository would assert that the Go
// code compiles while proving nothing about the behaviour that matters.
//
// Each call creates its own database, so tests never see each other's rows and
// can run in parallel.
package testdb

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// New returns a pool connected to a freshly migrated, uniquely named database.
// The database is dropped when the test finishes.
//
// The test is skipped when no database URL is configured, so `go test ./...`
// still works on a machine with no Postgres running.
func New(t *testing.T) *pgxpool.Pool {
	t.Helper()

	adminURL := databaseURL()
	if adminURL == "" {
		t.Skip("no TEST_DATABASE_URL or DATABASE_URL set; skipping database test")
	}

	ctx := context.Background()

	// uuid rather than a counter: parallel packages are separate processes and
	// would otherwise collide on the same name.
	name := "north_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")

	admin, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect to admin database: %v", err)
	}
	defer admin.Close()

	// The name is generated above, never user input, so interpolation is safe.
	// Postgres does not accept a placeholder in CREATE DATABASE.
	if _, err := admin.Exec(ctx, fmt.Sprintf("CREATE DATABASE %q", name)); err != nil {
		t.Fatalf("create test database: %v", err)
	}

	testURL := replaceDatabase(adminURL, name)
	migrate(t, testURL)

	pool, err := pgxpool.New(ctx, testURL)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()

		// A fresh admin connection: the deferred Close above has already run by
		// the time cleanup fires.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		dropper, err := pgxpool.New(cleanupCtx, adminURL)
		if err != nil {
			t.Logf("could not connect to drop test database %s: %v", name, err)
			return
		}
		defer dropper.Close()

		if _, err := dropper.Exec(cleanupCtx, fmt.Sprintf("DROP DATABASE IF EXISTS %q WITH (FORCE)", name)); err != nil {
			t.Logf("could not drop test database %s: %v", name, err)
		}
	})

	return pool
}

func migrate(t *testing.T, url string) {
	t.Helper()

	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open migration connection: %v", err)
	}
	defer db.Close()

	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatalf("set goose dialect: %v", err)
	}

	if err := goose.Up(db, migrationsDir(t)); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
}

// migrationsDir walks up from the test's working directory to the module root
// and returns its migrations directory. Tests run in their own package
// directory, so a relative path would differ per slice.
func migrationsDir(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "migrations")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find module root above the test directory")
		}
		dir = parent
	}
}

func databaseURL() string {
	if u := os.Getenv("TEST_DATABASE_URL"); u != "" {
		return u
	}
	return os.Getenv("DATABASE_URL")
}

// replaceDatabase swaps the database name in a Postgres URL, keeping the
// credentials, host, and query parameters.
func replaceDatabase(url, name string) string {
	base, query, hasQuery := strings.Cut(url, "?")

	slash := strings.LastIndex(base, "/")
	if slash == -1 {
		return url
	}
	base = base[:slash+1] + name

	if hasQuery {
		return base + "?" + query
	}
	return base
}

// ensures the pgx stdlib driver is linked, so sql.Open("pgx", ...) resolves.
var _ = stdlib.GetDefaultDriver
