package toolaudit

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	toolauditdb "github.com/NorthAIProject/north-client/internal/toolaudit/db"
)

type Repository struct {
	q *toolauditdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: toolauditdb.New(pool)}
}

func (r *Repository) Insert(ctx context.Context, e Execution) error {
	_, err := r.q.RecordToolExecution(ctx, toolauditdb.RecordToolExecutionParams{
		UserID: e.UserID,
		Tool:   e.Tool,
		// Left nil when there are none, so a tool taking no arguments records
		// null rather than an empty object it never received.
		Arguments: e.Arguments,
		Surface:   string(e.Surface),
		Outcome:   string(e.Outcome),
		Detail:    nilIfEmpty(e.Detail),
	})
	return apperr.Wrap(err, "record tool execution %q", e.Tool)
}

func (r *Repository) List(ctx context.Context, userID uuid.UUID, limit int) ([]Execution, error) {
	rows, err := r.q.ListToolExecutions(ctx, toolauditdb.ListToolExecutionsParams{
		UserID:   userID,
		RowLimit: int32(limit),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list tool executions")
	}

	out := make([]Execution, 0, len(rows))
	for _, row := range rows {
		e := Execution{
			ID:        row.ID,
			UserID:    row.UserID,
			Tool:      row.Tool,
			Arguments: row.Arguments,
			Surface:   Surface(row.Surface),
			Outcome:   Outcome(row.Outcome),
			CreatedAt: row.CreatedAt,
		}
		if row.Detail != nil {
			e.Detail = *row.Detail
		}
		out = append(out, e)
	}
	return out, nil
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
