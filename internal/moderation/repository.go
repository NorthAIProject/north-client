package moderation

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	moderationdb "github.com/NorthAIProject/north-client/internal/moderation/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// PostgresRepository keeps reports, moderation actions, accepted terms and
// suspensions in Postgres. It is the Repository the process wires into
// Service.
type PostgresRepository struct {
	pool *pgxpool.Pool
	q    *moderationdb.Queries
}

var _ Repository = (*PostgresRepository)(nil)

func NewRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool, q: moderationdb.New(pool)}
}

// FileReport stores a report in one transaction that holds the subject's
// lock. filed is false when the reporter had already reported it, and
// nothing else happens. Otherwise overThreshold is asked with the count of
// distinct reporters; when it says yes and hiding was not already decided,
// hide runs and the hide goes on the record, and hidden is true. Errors
// from the two callbacks are returned as they are.
func (r *PostgresRepository) FileReport(ctx context.Context, reporterID uuid.UUID, sub Subject, reason, note string,
	overThreshold func(reporters int64) (bool, error), hide func() error,
) (filed, hidden bool, err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, false, apperr.Wrap(err, "begin report")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := r.q.WithTx(tx)
	if err = q.LockSubject(ctx, moderationdb.LockSubjectParams{SubjectKind: sub.Kind, SubjectID: sub.ID.String()}); err != nil {
		return false, false, apperr.Wrap(err, "lock subject")
	}

	// A subject with no author, a coach message for one, has nobody to
	// point the report at.
	var author *uuid.UUID
	if sub.AuthorID != uuid.Nil {
		author = &sub.AuthorID
	}
	inserted, err := q.InsertReport(ctx, moderationdb.InsertReportParams{
		ReporterID: reporterID, SubjectKind: sub.Kind, SubjectID: sub.ID,
		SubjectAuthorID: author, ScopeID: sub.ScopeID, Reason: reason, Note: note,
	})
	if err != nil {
		return false, false, apperr.Wrap(err, "insert report")
	}
	if inserted == 0 {
		return false, false, nil
	}
	count, err := q.CountReporters(ctx, moderationdb.CountReportersParams{SubjectKind: sub.Kind, SubjectID: sub.ID})
	if err != nil {
		return false, false, apperr.Wrap(err, "count reporters")
	}
	over, err := overThreshold(count)
	if err != nil {
		return false, false, err
	}
	if over {
		decided, decidedErr := q.HideDecided(ctx, moderationdb.HideDecidedParams{SubjectKind: sub.Kind, SubjectID: sub.ID})
		if decidedErr != nil {
			return false, false, apperr.Wrap(decidedErr, "hide decided")
		}
		if !decided {
			if err = hide(); err != nil {
				return false, false, err
			}
			if err = q.InsertAction(ctx, moderationdb.InsertActionParams{
				SubjectKind: sub.Kind, SubjectID: sub.ID, Action: "hide", Reason: "reports",
			}); err != nil {
				return false, false, apperr.Wrap(err, "record hide")
			}
			hidden = true
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return false, false, apperr.Wrap(err, "commit report")
	}
	return true, hidden, nil
}

// OpenReports is every subject with open reports, oldest first, without
// excerpts: those come from the owning slice.
func (r *PostgresRepository) OpenReports(ctx context.Context) ([]ReportGroup, error) {
	rows, err := r.q.OpenReports(ctx)
	if err != nil {
		return nil, apperr.Wrap(err, "open reports")
	}
	out := make([]ReportGroup, 0, len(rows))
	for _, row := range rows {
		g := ReportGroup{Kind: row.SubjectKind, SubjectID: row.SubjectID, Count: int(row.N), Hidden: row.Hidden, FirstAt: row.FirstAt, Reasons: map[string]int{}}
		if row.AuthorID != uuid.Nil {
			g.AuthorID = &row.AuthorID
		}
		for _, reason := range row.Reasons {
			g.Reasons[reason]++
		}
		out = append(out, g)
	}
	return out, nil
}

// Decide records an operator's action (any but dismiss) and closes the
// subject's open reports with status, in one transaction.
func (r *PostgresRepository) Decide(ctx context.Context, operatorID uuid.UUID, kind string, subjectID uuid.UUID, action, status string) error {
	return r.inTx(ctx, "decide", func(q *moderationdb.Queries) error {
		if action != DecideDismiss {
			if err := q.InsertAction(ctx, moderationdb.InsertActionParams{
				ActorID: &operatorID, SubjectKind: kind, SubjectID: subjectID, Action: action, Reason: "operator",
			}); err != nil {
				return err
			}
		}
		return q.CloseReports(ctx, moderationdb.CloseReportsParams{Status: status, SubjectKind: kind, SubjectID: subjectID})
	})
}

// ReportedBy says which of ids reporterID has reported.
func (r *PostgresRepository) ReportedBy(ctx context.Context, reporterID uuid.UUID, kind string, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	rows, err := r.q.ReportedBy(ctx, moderationdb.ReportedByParams{ReporterID: reporterID, SubjectKind: kind, Ids: ids})
	if err != nil {
		return nil, apperr.Wrap(err, "reported by")
	}
	out := make(map[uuid.UUID]bool, len(rows))
	for _, id := range rows {
		out[id] = true
	}
	return out, nil
}

// TermsAccepted says whether the person accepted the current TermsVersion.
func (r *PostgresRepository) TermsAccepted(ctx context.Context, userID uuid.UUID) (bool, error) {
	ok, err := r.q.TermsAccepted(ctx, moderationdb.TermsAcceptedParams{UserID: userID, Version: TermsVersion})
	if err != nil {
		return false, apperr.Wrap(err, "terms accepted")
	}
	return ok, nil
}

// AcceptTerms records the person accepting the current TermsVersion.
func (r *PostgresRepository) AcceptTerms(ctx context.Context, userID uuid.UUID) error {
	if err := r.q.AcceptTerms(ctx, moderationdb.AcceptTermsParams{UserID: userID, Version: TermsVersion}); err != nil {
		return apperr.Wrap(err, "accept terms")
	}
	return nil
}

// Suspend stores the suspension and its action in one transaction.
func (r *PostgresRepository) Suspend(ctx context.Context, actorID *uuid.UUID, userID uuid.UUID, until *time.Time, reason string) error {
	return r.inTx(ctx, "suspend", func(q *moderationdb.Queries) error {
		if err := q.Suspend(ctx, moderationdb.SuspendParams{UserID: userID, Until: until, Reason: reason}); err != nil {
			return err
		}
		return q.InsertAction(ctx, moderationdb.InsertActionParams{
			ActorID: actorID, SubjectKind: "user", SubjectID: userID, Action: "suspend", Reason: reason,
		})
	})
}

// Unsuspend lifts a suspension and records it, in one transaction; lifting
// none is a no-op.
func (r *PostgresRepository) Unsuspend(ctx context.Context, actorID *uuid.UUID, userID uuid.UUID) error {
	return r.inTx(ctx, "unsuspend", func(q *moderationdb.Queries) error {
		n, err := q.Unsuspend(ctx, userID)
		if err != nil || n == 0 {
			return err
		}
		return q.InsertAction(ctx, moderationdb.InsertActionParams{
			ActorID: actorID, SubjectKind: "user", SubjectID: userID, Action: "unsuspend",
		})
	})
}

func (r *PostgresRepository) Suspended(ctx context.Context, userID uuid.UUID, now time.Time) (bool, error) {
	ok, err := r.q.Suspended(ctx, moderationdb.SuspendedParams{UserID: userID, Now: now})
	if err != nil {
		return false, apperr.Wrap(err, "suspended")
	}
	return ok, nil
}

func (r *PostgresRepository) inTx(ctx context.Context, what string, fn func(*moderationdb.Queries) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return apperr.Wrap(err, "begin %s", what)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = fn(r.q.WithTx(tx)); err != nil {
		return apperr.Wrap(err, "%s", what)
	}
	if err = tx.Commit(ctx); err != nil {
		return apperr.Wrap(err, "commit %s", what)
	}
	return nil
}
