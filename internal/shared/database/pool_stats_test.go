package database_test

import (
	"context"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/database"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
)

// The mapping from pgx's getters to what metrics exports, on a real pool: a
// held connection has to show up as acquired, and the limit as the pool's own.
func TestPoolStatsReadThePool(t *testing.T) {
	pool := testdb.New(t)

	conn, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()

	got := database.PoolStats(pool)
	if got.AcquiredConns < 1 {
		t.Errorf("AcquiredConns = %d with a connection held", got.AcquiredConns)
	}
	if got.MaxConns != pool.Config().MaxConns {
		t.Errorf("MaxConns = %d, want the pool's %d", got.MaxConns, pool.Config().MaxConns)
	}
}
