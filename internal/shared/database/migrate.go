package database

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/NorthAIProject/north-client/migrations"
)

// Migrate applies all pending goose migrations embedded in the binary.
//
// Safe to call from every process that starts (web, worker): a Postgres session
// lock keeps concurrent boots from racing on the same schema change.
//
// Call after the database is reachable and before any repository work.
func Migrate(ctx context.Context, databaseURL string) error {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return fmt.Errorf("open migration connection: %w", err)
	}
	defer func() { _ = db.Close() }()

	if pingErr := db.PingContext(ctx); pingErr != nil {
		return fmt.Errorf("ping database for migrations: %w", pingErr)
	}

	// Session locker: multi-replica deploys all call Migrate on start; only one
	// applies while the others wait, then see an empty pending set.
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("migration session locker: %w", err)
	}

	provider, err := goose.NewProvider(
		goose.DialectPostgres,
		db,
		migrations.FS,
		goose.WithSessionLocker(locker),

		// Out-of-order migrations are applied rather than refused.
		//
		// Without this, two branches developed in parallel cannot both be
		// merged: whichever lands second carries a version lower than one the
		// database has already applied, and every boot fails with "missing
		// (out-of-order) migration" until someone renumbers files by hand.
		// That is not a hypothetical — it happened twice in one day here.
		//
		// The trade this accepts: migrations are no longer guaranteed to run
		// in the order they were written, only that each runs exactly once.
		// That is already true of any system with branches, and it is what
		// the numbering was pretending to guarantee rather than actually
		// guaranteeing. Migrations must therefore not depend on each other's
		// ordering beyond what the schema itself enforces — which is the
		// discipline a migration should have anyway.
		goose.WithAllowOutofOrder(true),
	)
	if err != nil {
		return fmt.Errorf("migration provider: %w", err)
	}

	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	return nil
}

// ensures the pgx database/sql driver is registered for sql.Open("pgx", …).
var _ = stdlib.GetDefaultDriver
