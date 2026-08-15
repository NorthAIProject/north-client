package decisions

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	decisionsdb "github.com/NorthAIProject/north-client/internal/decisions/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *decisionsdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: decisionsdb.New(pool)}
}

func (r *Repository) Create(ctx context.Context, userID uuid.UUID, title, options, rationale, outcome string) (Decision, error) {
	row, err := r.q.CreateDecision(ctx, decisionsdb.CreateDecisionParams{
		UserID:    userID,
		Title:     title,
		Options:   options,
		Rationale: rationale,
		Outcome:   outcome,
	})
	if err != nil {
		return Decision{}, apperr.Wrap(err, "create decision")
	}
	return fromDB(row), nil
}

func (r *Repository) Get(ctx context.Context, id, userID uuid.UUID) (Decision, error) {
	row, err := r.q.GetDecision(ctx, decisionsdb.GetDecisionParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Decision{}, apperr.ErrNotFound
		}
		return Decision{}, apperr.Wrap(err, "get decision")
	}
	return fromDB(row), nil
}

func (r *Repository) List(ctx context.Context, userID uuid.UUID, limit int) ([]Decision, error) {
	rows, err := r.q.ListDecisions(ctx, decisionsdb.ListDecisionsParams{
		UserID: userID,
		Limit:  int32(limit),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list decisions")
	}
	return fromDBList(rows), nil
}

func (r *Repository) Update(ctx context.Context, id, userID uuid.UUID, title, options, rationale, outcome string) (Decision, error) {
	row, err := r.q.UpdateDecision(ctx, decisionsdb.UpdateDecisionParams{
		ID:        id,
		UserID:    userID,
		Title:     title,
		Options:   options,
		Rationale: rationale,
		Outcome:   outcome,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Decision{}, apperr.ErrNotFound
		}
		return Decision{}, apperr.Wrap(err, "update decision")
	}
	return fromDB(row), nil
}

func (r *Repository) Delete(ctx context.Context, id, userID uuid.UUID) error {
	n, err := r.q.DeleteDecision(ctx, decisionsdb.DeleteDecisionParams{ID: id, UserID: userID})
	if err != nil {
		return apperr.Wrap(err, "delete decision")
	}
	if n == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func fromDBList(rows []decisionsdb.Decision) []Decision {
	out := make([]Decision, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDB(row))
	}
	return out
}

func fromDB(row decisionsdb.Decision) Decision {
	return Decision{
		ID:        row.ID,
		UserID:    row.UserID,
		Title:     row.Title,
		Options:   row.Options,
		Rationale: row.Rationale,
		Outcome:   row.Outcome,
		DecidedAt: row.DecidedAt,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}
