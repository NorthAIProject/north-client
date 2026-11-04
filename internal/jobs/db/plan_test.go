package jobsdb

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
)

// planFor explains a query with sequential scans switched off, because on the
// handful of rows a test has the planner would rightly prefer one and the plan
// would say nothing about what happens on a real table.
func planFor(t *testing.T, query string, args ...any) string {
	t.Helper()
	ctx := context.Background()

	tx, err := testdb.New(t).Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err = tx.Exec(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatalf("disable seqscan: %v", err)
	}

	rows, err := tx.Query(ctx, "EXPLAIN "+query, args...)
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	lines, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("read plan: %v", err)
	}
	return strings.Join(lines, "\n")
}

// assertIndexSeeksOnUser fails unless the named index narrows by user. Naming
// the index alone is not enough: it leads with kind, so a query that cannot use
// the user column still scans the index, reading every job of that kind.
func assertIndexSeeksOnUser(t *testing.T, plan, index string) {
	t.Helper()
	if !strings.Contains(plan, index) {
		t.Fatalf("plan does not use %s:\n%s", index, plan)
	}
	for line := range strings.Lines(plan) {
		if strings.Contains(line, "Index Cond") && strings.Contains(line, "user_id") {
			return
		}
	}
	t.Fatalf("%s is not narrowed by user_id:\n%s", index, plan)
}

func TestHasPendingJobForUserSeeksTheActiveIndex(t *testing.T) {
	plan := planFor(t, hasPendingJobForUser, "embed_chunks", uuid.NewString())
	assertIndexSeeksOnUser(t, plan, "jobs_active_kind_user_idx")
}

func TestHasRecentFailedJobForUserSeeksTheFailedIndex(t *testing.T) {
	plan := planFor(t, hasRecentFailedJobForUser, "embed_chunks", uuid.NewString(), 3600.0)
	assertIndexSeeksOnUser(t, plan, "jobs_failed_kind_user_idx")
}
