package database_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/NorthAIProject/north-client/migrations"
)

const (
	beforePeriodicDedupe = 20261104100100 // the last index migration before it
	periodicDedupe       = 20261104101000 // one live copy of each periodic sweep
	periodicDedupeIndex  = "jobs_periodic_active_kind_idx"
)

// The unique index cannot build over duplicates, and a production queue can hold
// them on any deploy. The migration has to close the surplus sweeps, keep the
// right copy, touch nothing that carries work, delete nothing, and recover from a
// previous attempt that failed partway.
func TestPeriodicDedupeMigrationHandlesExistingDuplicates(t *testing.T) {
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

	const dbName = "north_jobs_periodic_dedupe_check"
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
	if _, err = provider.UpTo(ctx, beforePeriodicDedupe); err != nil {
		t.Fatalf("migrate to %d: %v", beforePeriodicDedupe, err)
	}

	const userPayload = `{"user_id": "00000000-0000-0000-0000-000000000001"}`
	seed := []struct {
		id, kind, payload, status, age string
		wantStatus                     string
	}{
		// A running copy wins over newer pending ones: a worker may be partway
		// through it.
		{"00000000-0000-0000-0000-00000000a001", "sweep_a", `{}`, "running", "3 hours", "running"},
		{"00000000-0000-0000-0000-00000000a002", "sweep_a", `{}`, "pending", "2 hours", "done"},
		{"00000000-0000-0000-0000-00000000a003", "sweep_a", `{}`, "pending", "1 hour", "done"},

		// Among pending copies, the most recently touched stays.
		{"00000000-0000-0000-0000-00000000b001", "sweep_b", `{}`, "pending", "2 hours", "done"},
		{"00000000-0000-0000-0000-00000000b002", "sweep_b", `{}`, "pending", "1 hour", "pending"},

		// Two running, as a rolling update can leave. The newer is the live one.
		{"00000000-0000-0000-0000-00000000c001", "sweep_c", `{}`, "running", "2 hours", "done"},
		{"00000000-0000-0000-0000-00000000c002", "sweep_c", `{}`, "running", "1 hour", "running"},

		// A lone copy is left alone.
		{"00000000-0000-0000-0000-00000000d001", "sweep_d", `{}`, "pending", "1 hour", "pending"},

		// Jobs with a payload carry their own work and are never touched, even
		// when they look identical.
		{"00000000-0000-0000-0000-00000000e001", "embed_chunks", userPayload, "pending", "2 hours", "pending"},
		{"00000000-0000-0000-0000-00000000e002", "embed_chunks", userPayload, "pending", "1 hour", "pending"},

		// Finished history is not part of the index.
		{"00000000-0000-0000-0000-00000000f001", "sweep_f", `{}`, "done", "2 hours", "done"},
		{"00000000-0000-0000-0000-00000000f002", "sweep_f", `{}`, "failed", "1 hour", "failed"},
	}
	for _, row := range seed {
		if _, err = db.ExecContext(ctx,
			`INSERT INTO jobs (id, kind, payload, status, updated_at)
			 VALUES ($1::uuid, $2, $3::jsonb, $4, now() - $5::interval)`,
			row.id, row.kind, row.payload, row.status, row.age,
		); err != nil {
			t.Fatalf("seed %s: %v", row.id, err)
		}
	}
	if _, err = db.ExecContext(ctx,
		`UPDATE jobs SET last_error = 'provider down' WHERE id = '00000000-0000-0000-0000-00000000b001'`,
	); err != nil {
		t.Fatalf("seed last_error: %v", err)
	}

	// The state a failed attempt leaves: this very build, run over duplicates,
	// fails and leaves an invalid index behind under the name.
	_, err = db.ExecContext(ctx, `CREATE UNIQUE INDEX CONCURRENTLY `+periodicDedupeIndex+`
		ON jobs (kind) WHERE status IN ('pending', 'running') AND payload = '{}'::jsonb`)
	if err == nil {
		t.Fatal("the build succeeded over duplicates, so this test is not exercising the migration")
	}
	if valid, found := indexValid(ctx, t, db); !found || valid {
		t.Fatalf("expected an invalid leftover index: found=%v valid=%v", found, valid)
	}

	if _, err = provider.ApplyVersion(ctx, periodicDedupe, true); err != nil {
		t.Fatalf("migration did not recover from the duplicates: %v", err)
	}

	if valid, found := indexValid(ctx, t, db); !found || !valid {
		t.Fatalf("index after migration: found=%v valid=%v", found, valid)
	}

	var total int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM jobs`).Scan(&total); err != nil {
		t.Fatalf("count: %v", err)
	}
	if total != len(seed) {
		t.Errorf("rows after migration = %d, want %d: the migration must not delete", total, len(seed))
	}

	for _, row := range seed {
		var status, lastError string
		if err = db.QueryRowContext(ctx,
			`SELECT status, last_error FROM jobs WHERE id = $1::uuid`, row.id,
		).Scan(&status, &lastError); err != nil {
			t.Fatalf("read %s: %v", row.id, err)
		}
		if status != row.wantStatus {
			t.Errorf("%s (%s, was %s) = %s, want %s", row.id, row.kind, row.status, status, row.wantStatus)
		}
		closed := row.status != row.wantStatus
		if closed != strings.HasPrefix(lastError, "superseded:") {
			t.Errorf("%s last_error = %q, closed = %v", row.id, lastError, closed)
		}
	}

	var kept string
	if err = db.QueryRowContext(ctx,
		`SELECT last_error FROM jobs WHERE id = '00000000-0000-0000-0000-00000000b001'`,
	).Scan(&kept); err != nil {
		t.Fatalf("read last_error: %v", err)
	}
	if !strings.Contains(kept, "provider down") {
		t.Errorf("last_error = %q: closing a row lost its previous error", kept)
	}

	// Down and up again leaves a valid index and changes nothing further.
	if _, err = provider.ApplyVersion(ctx, periodicDedupe, false); err != nil {
		t.Fatalf("down: %v", err)
	}
	if _, found := indexValid(ctx, t, db); found {
		t.Error("the index survived the down migration")
	}
	if _, err = provider.ApplyVersion(ctx, periodicDedupe, true); err != nil {
		t.Fatalf("up again: %v", err)
	}
	if valid, found := indexValid(ctx, t, db); !found || !valid {
		t.Fatalf("index after up again: found=%v valid=%v", found, valid)
	}
}

func indexValid(ctx context.Context, t *testing.T, db *sql.DB) (valid, found bool) {
	t.Helper()
	err := db.QueryRowContext(ctx,
		`SELECT i.indisvalid FROM pg_index i
		 JOIN pg_class c ON c.oid = i.indexrelid
		 WHERE c.relname = $1`, periodicDedupeIndex,
	).Scan(&valid)
	if err == sql.ErrNoRows {
		return false, false
	}
	if err != nil {
		t.Fatalf("read index state: %v", err)
	}
	return valid, true
}
