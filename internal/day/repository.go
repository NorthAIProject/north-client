package day

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/day/day"
	daydb "github.com/NorthAIProject/north-client/internal/day/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Repository owns day_rules. The rest of a day is read through other slices'
// services; this slice stores only what is its own.
type Repository struct {
	q *daydb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: daydb.New(pool)}
}

func (r *Repository) ListRules(ctx context.Context, userID uuid.UUID) ([]day.Rule, error) {
	rows, err := r.q.ListDayRules(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "list day rules")
	}
	out := make([]day.Rule, 0, len(rows))
	for _, row := range rows {
		out = append(out, ruleFromDB(row))
	}
	return out, nil
}

func (r *Repository) UpsertRule(ctx context.Context, userID uuid.UUID, rule day.Rule) (day.Rule, error) {
	row, err := r.q.UpsertDayRule(ctx, daydb.UpsertDayRuleParams{
		UserID:  userID,
		Kind:    string(rule.Kind),
		AtTime:  rule.At,
		Enabled: rule.Enabled,
	})
	if err != nil {
		return day.Rule{}, apperr.Wrap(err, "upsert day rule")
	}
	return ruleFromDB(row), nil
}

func (r *Repository) DeleteRule(ctx context.Context, userID uuid.UUID, kind day.RuleKind) error {
	if err := r.q.DeleteDayRule(ctx, daydb.DeleteDayRuleParams{UserID: userID, Kind: string(kind)}); err != nil {
		return apperr.Wrap(err, "delete day rule")
	}
	return nil
}

func ruleFromDB(row daydb.DayRule) day.Rule {
	return day.Rule{Kind: day.RuleKind(row.Kind), At: row.AtTime, Enabled: row.Enabled}
}
