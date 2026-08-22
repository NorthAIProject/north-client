package spend

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	spenddb "github.com/NorthAIProject/north-client/internal/spend/db"
)

// Repository owns the ledger table. No sqlc or pgx type escapes it.
type Repository struct {
	q   *spenddb.Queries
	log *slog.Logger
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: spenddb.New(pool), log: slog.Default()}
}

// WithLogger replaces the logger used for recording failures.
func (r *Repository) WithLogger(log *slog.Logger) *Repository {
	if log == nil {
		return r
	}
	return &Repository{q: r.q, log: log}
}

// Record appends one generation and swallows the error.
//
// It implements Recorder, which returns nothing on purpose. By the time this
// runs the model has already answered and the user has already been served; a
// failed insert must not turn a successful reply into an error. The cost of
// that choice is that a broken ledger is silent to the user, so it is loud in
// the log instead — and CountUnpriced exists to make gaps visible in the report
// rather than only in a log nobody reads.
func (r *Repository) Record(ctx context.Context, g Generation) {
	if err := r.q.RecordGeneration(ctx, spenddb.RecordGenerationParams{
		UserID:       g.UserID,
		Surface:      surfaceOrUnknown(g.Surface),
		Provider:     g.Provider,
		Model:        nilIfEmpty(g.Model),
		InputTokens:  int32(g.InputTokens),
		OutputTokens: int32(g.OutputTokens),
		CostMicros:   g.CostMicros,
		Priced:       g.Priced,
		Byok:         g.BYOK,
	}); err != nil {
		r.log.ErrorContext(ctx, "could not record ai spend",
			slog.String("surface", g.Surface),
			slog.String("provider", g.Provider),
			slog.String("model", g.Model),
			slog.Any("error", err))
	}
}

// ByUser totals spend per account over the window.
func (r *Repository) ByUser(ctx context.Context, window Range, billableOnly bool) ([]UserSpend, error) {
	rows, err := r.q.SpendByUser(ctx, spenddb.SpendByUserParams{
		FromTime:     window.From,
		ToTime:       window.To,
		BillableOnly: billableOnly,
	})
	if err != nil {
		return nil, apperr.Wrap(err, "spend by user")
	}
	out := make([]UserSpend, 0, len(rows))
	for _, row := range rows {
		out = append(out, UserSpend{
			UserID:       row.UserID,
			Generations:  row.Generations,
			InputTokens:  row.InputTokens,
			OutputTokens: row.OutputTokens,
			CostMicros:   row.CostMicros,
		})
	}
	return out, nil
}

// ByModel totals spend per provider and model over the window.
func (r *Repository) ByModel(ctx context.Context, window Range, billableOnly bool) ([]ModelSpend, error) {
	rows, err := r.q.SpendByModel(ctx, spenddb.SpendByModelParams{
		FromTime:     window.From,
		ToTime:       window.To,
		BillableOnly: billableOnly,
	})
	if err != nil {
		return nil, apperr.Wrap(err, "spend by model")
	}
	out := make([]ModelSpend, 0, len(rows))
	for _, row := range rows {
		out = append(out, ModelSpend{
			Provider:     row.Provider,
			Model:        deref(row.Model),
			Generations:  row.Generations,
			InputTokens:  row.InputTokens,
			OutputTokens: row.OutputTokens,
			CostMicros:   row.CostMicros,
		})
	}
	return out, nil
}

// BySurface totals spend per surface over the window.
func (r *Repository) BySurface(ctx context.Context, window Range, billableOnly bool) ([]SurfaceSpend, error) {
	rows, err := r.q.SpendBySurface(ctx, spenddb.SpendBySurfaceParams{
		FromTime:     window.From,
		ToTime:       window.To,
		BillableOnly: billableOnly,
	})
	if err != nil {
		return nil, apperr.Wrap(err, "spend by surface")
	}
	out := make([]SurfaceSpend, 0, len(rows))
	for _, row := range rows {
		out = append(out, SurfaceSpend{
			Surface:      row.Surface,
			Generations:  row.Generations,
			InputTokens:  row.InputTokens,
			OutputTokens: row.OutputTokens,
			CostMicros:   row.CostMicros,
		})
	}
	return out, nil
}

// CountUnpriced reports how many billable calls landed with no price. A
// non-zero answer means the pricing table is missing a model and every total is
// an understatement — which is the one way a spend report can be confidently
// wrong rather than merely incomplete.
func (r *Repository) CountUnpriced(ctx context.Context, window Range) (int64, error) {
	n, err := r.q.CountUnpricedGenerations(ctx, spenddb.CountUnpricedGenerationsParams{
		FromTime: window.From,
		ToTime:   window.To,
	})
	if err != nil {
		return 0, apperr.Wrap(err, "count unpriced generations")
	}
	return n, nil
}

func surfaceOrUnknown(s string) string {
	if s == "" {
		return SurfaceUnknown
	}
	return s
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Assert the repository satisfies the interface the metering decorator depends
// on, so a signature change breaks the build here rather than at wiring time.
var _ Recorder = (*Repository)(nil)
