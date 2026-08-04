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
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database for migrations: %w", err)
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
