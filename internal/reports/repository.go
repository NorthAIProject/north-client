package reports

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	reportsdb "github.com/NorthAIProject/north-client/internal/reports/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *reportsdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: reportsdb.New(pool)}
}

func (r *Repository) Create(ctx context.Context, userID uuid.UUID, period Period, kind Kind) (Report, error) {
	row, err := r.q.CreateReport(ctx, reportsdb.CreateReportParams{
		UserID:      userID,
		Kind:        string(kind),
		PeriodStart: toDate(period.Start),
		PeriodEnd:   toDate(period.End),
		Title:       period.Title,
		Status:      string(StatusPending),
	})
	if err != nil {
		return Report{}, apperr.Wrap(err, "create report")
	}
	return fromDB(row), nil
}

func (r *Repository) Get(ctx context.Context, id, userID uuid.UUID) (Report, error) {
	row, err := r.q.GetReport(ctx, reportsdb.GetReportParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Report{}, apperr.ErrNotFound
		}
		return Report{}, apperr.Wrap(err, "get report")
	}
	return fromDB(row), nil
}

func (r *Repository) ActiveForWeek(ctx context.Context, userID uuid.UUID, kind Kind, start time.Time) (Report, error) {
	row, err := r.q.GetActiveReportForWeek(ctx, reportsdb.GetActiveReportForWeekParams{
		UserID:      userID,
		Kind:        string(kind),
		PeriodStart: toDate(start),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Report{}, apperr.ErrNotFound
		}
		return Report{}, apperr.Wrap(err, "get active report")
	}
	return fromDB(row), nil
}

// List returns the person's reports, newest period first. An empty kind means
// every kind; pass one to keep briefings out of the weekly review list.
func (r *Repository) List(ctx context.Context, userID uuid.UUID, kind Kind, includeArchived bool, limit int) ([]Report, error) {
	if limit <= 0 {
		limit = 50
	}
	var filter *string
	if kind != "" {
		k := string(kind)
		filter = &k
	}
	rows, err := r.q.ListReports(ctx, reportsdb.ListReportsParams{
		UserID:          userID,
		IncludeArchived: includeArchived,
		Limit:           int32(limit),
		Kind:            filter,
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list reports")
	}
	out := make([]Report, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDB(row))
	}
	return out, nil
}

func (r *Repository) Archive(ctx context.Context, id, userID uuid.UUID) (Report, error) {
	row, err := r.q.ArchiveReport(ctx, reportsdb.ArchiveReportParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Report{}, apperr.ErrNotFound
		}
		return Report{}, apperr.Wrap(err, "archive report")
	}
	return fromDB(row), nil
}

func (r *Repository) MarkPending(ctx context.Context, id, userID uuid.UUID) (Report, error) {
	row, err := r.q.MarkReportPending(ctx, reportsdb.MarkReportPendingParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Report{}, apperr.ErrNotFound
		}
		return Report{}, apperr.Wrap(err, "mark report pending")
	}
	return fromDB(row), nil
}

func (r *Repository) SaveGenerated(ctx context.Context, id, userID uuid.UUID, body string) (Report, error) {
	row, err := r.q.SaveGeneratedReport(ctx, reportsdb.SaveGeneratedReportParams{
		ID:     id,
		UserID: userID,
		Body:   body,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Report{}, apperr.ErrNotFound
		}
		return Report{}, apperr.Wrap(err, "save generated report")
	}
	return fromDB(row), nil
}

func (r *Repository) MarkFailed(ctx context.Context, id, userID uuid.UUID, reason string) (Report, error) {
	row, err := r.q.MarkReportFailed(ctx, reportsdb.MarkReportFailedParams{
		ID:        id,
		UserID:    userID,
		LastError: reason,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Report{}, apperr.ErrNotFound
		}
		return Report{}, apperr.Wrap(err, "mark report failed")
	}
	return fromDB(row), nil
}

// LatestReady is filtered by kind on purpose. The table holds both weekly
// reviews and daily briefings, so an unfiltered "newest ready row" would hand
// the dashboard card whichever kind happened to be generated last.
func (r *Repository) LatestReady(ctx context.Context, userID uuid.UUID, kind Kind) (Report, error) {
	row, err := r.q.LatestReadyReport(ctx, reportsdb.LatestReadyReportParams{
		UserID: userID,
		Kind:   string(kind),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Report{}, apperr.ErrNotFound
		}
		return Report{}, apperr.Wrap(err, "latest ready report")
	}
	return fromDB(row), nil
}

func toDate(t time.Time) pgtype.Date {
	if t.IsZero() {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: t, Valid: true}
}

func fromDate(d pgtype.Date) time.Time {
	if !d.Valid {
		return time.Time{}
	}
	return d.Time
}

// SetHelpful records whether a report was worth reading, or clears the answer
// when helpful is nil.
func (r *Repository) SetHelpful(ctx context.Context, id, userID uuid.UUID, helpful *bool) (Report, error) {
	row, err := r.q.SetReportHelpful(ctx, reportsdb.SetReportHelpfulParams{
		ID:      id,
		UserID:  userID,
		Helpful: helpful,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Report{}, apperr.ErrNotFound
	}
	if err != nil {
		return Report{}, apperr.Wrap(err, "set report helpful")
	}
	return fromDB(row), nil
}

func fromDB(row reportsdb.Report) Report {
	r := Report{
		ID:          row.ID,
		UserID:      row.UserID,
		Kind:        Kind(row.Kind),
		PeriodStart: fromDate(row.PeriodStart),
		PeriodEnd:   fromDate(row.PeriodEnd),
		Title:       row.Title,
		Body:        row.Body,
		Status:      Status(row.Status),
		LastError:   row.LastError,
		GeneratedAt: row.GeneratedAt,
		ArchivedAt:  row.ArchivedAt,
		Helpful:     row.Helpful,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
	return r
}
