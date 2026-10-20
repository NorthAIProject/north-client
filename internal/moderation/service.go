package moderation

import (
	"context"
	"log/slog"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	moderationdb "github.com/NorthAIProject/north-client/internal/moderation/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Service struct {
	pool      *pgxpool.Pool
	q         *moderationdb.Queries
	hiders    map[string]Hider
	operators []uuid.UUID
	pushers   []ThreadSender
	log       *slog.Logger
	now       func() time.Time
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, q: moderationdb.New(pool), hiders: map[string]Hider{}, log: slog.Default(), now: time.Now}
}

// WithOperators names who reviews reports and how they hear about new ones:
// each pusher (APNs, web push) gets every new report. Nil pushers are
// skipped.
func (s *Service) WithOperators(ids []uuid.UUID, pushers ...ThreadSender) *Service {
	s.operators = ids
	for _, p := range pushers {
		if p != nil {
			s.pushers = append(s.pushers, p)
		}
	}
	return s
}

// WithLogger sets where best-effort failures are logged.
func (s *Service) WithLogger(log *slog.Logger) *Service { s.log = log; return s }

// IsOperator says whether userID reviews reports.
func (s *Service) IsOperator(userID uuid.UUID) bool { return slices.Contains(s.operators, userID) }

// WithClock fixes now, for tests.
func (s *Service) WithClock(now func() time.Time) *Service { s.now = now; return s }

// Register names the slice that owns a kind of subject. Call it while wiring,
// before serving: the map is not guarded.
func (s *Service) Register(kind string, h Hider) { s.hiders[kind] = h }

// Report records that reporterID flagged sub. Reporting the same thing twice
// is a no-op. When the report brings the subject to its slice's threshold of
// distinct reporters, the subject is hidden for everybody and hidden is true.
func (s *Service) Report(ctx context.Context, reporterID uuid.UUID, sub Subject, reason, note string) (hidden bool, err error) {
	h, ok := s.hiders[sub.Kind]
	if !ok {
		return false, apperr.ErrNotFound
	}
	if reporterID == sub.AuthorID {
		return false, apperr.FieldErrors{}.Add("report", "You can't report your own post.")
	}
	if !reasons[reason] {
		return false, apperr.FieldErrors{}.Add("reason", "Choose a reason.")
	}
	if utf8.RuneCountInString(note) > 500 {
		return false, apperr.FieldErrors{}.Add("note", "Keep the note under 500 characters.")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, apperr.Wrap(err, "begin report")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	if err = q.LockSubject(ctx, moderationdb.LockSubjectParams{SubjectKind: sub.Kind, SubjectID: sub.ID.String()}); err != nil {
		return false, apperr.Wrap(err, "lock subject")
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
		return false, apperr.Wrap(err, "insert report")
	}
	if inserted == 0 {
		return false, nil
	}
	count, err := q.CountReporters(ctx, moderationdb.CountReportersParams{SubjectKind: sub.Kind, SubjectID: sub.ID})
	if err != nil {
		return false, apperr.Wrap(err, "count reporters")
	}
	threshold, err := h.Threshold(ctx, sub.ID)
	if err != nil {
		return false, err
	}
	// At or past the threshold, hide unless hiding was already decided: by
	// an earlier report, or by a moderator who unhid it. The threshold can
	// fall below the count when a crew shrinks, so this is not equality.
	if count >= int64(threshold) {
		decided, decidedErr := q.HideDecided(ctx, moderationdb.HideDecidedParams{SubjectKind: sub.Kind, SubjectID: sub.ID})
		if decidedErr != nil {
			return false, apperr.Wrap(decidedErr, "hide decided")
		}
		if decided {
			if err = tx.Commit(ctx); err != nil {
				return false, apperr.Wrap(err, "commit report")
			}
			s.tellOperators(ctx, reason)
			return false, nil
		}
		// The slice hides in its own tables, outside this transaction; Hide
		// is idempotent, so a failed commit after it only loses the record.
		if err = h.Hide(ctx, sub.ID); err != nil {
			return false, err
		}
		if err = q.InsertAction(ctx, moderationdb.InsertActionParams{
			SubjectKind: sub.Kind, SubjectID: sub.ID, Action: "hide", Reason: "reports",
		}); err != nil {
			return false, apperr.Wrap(err, "record hide")
		}
		hidden = true
	}
	if err = tx.Commit(ctx); err != nil {
		return false, apperr.Wrap(err, "commit report")
	}
	s.tellOperators(ctx, reason)
	return hidden, nil
}

// tellOperators pushes every operator about a new report. Best effort: the
// report is stored either way, and the queue shows it.
func (s *Service) tellOperators(ctx context.Context, reason string) {
	for _, op := range s.operators {
		for _, p := range s.pushers {
			if _, err := p.SendThread(ctx, op, "New report", reason, "/app/ops/moderation", "", "moderation"); err != nil {
				s.log.Warn("moderation: could not push operator", slog.String("operator_id", op.String()), slog.Any("error", err))
			}
		}
	}
}

// OpenReports is the operator queue: every subject with open reports,
// oldest first.
func (s *Service) OpenReports(ctx context.Context) ([]ReportGroup, error) {
	rows, err := s.q.OpenReports(ctx)
	if err != nil {
		return nil, apperr.Wrap(err, "open reports")
	}
	out := make([]ReportGroup, 0, len(rows))
	for _, r := range rows {
		g := ReportGroup{Kind: r.SubjectKind, SubjectID: r.SubjectID, Count: int(r.N), Hidden: r.Hidden, FirstAt: r.FirstAt, Reasons: map[string]int{}}
		if r.AuthorID != uuid.Nil {
			g.AuthorID = &r.AuthorID
		}
		for _, reason := range r.Reasons {
			g.Reasons[reason]++
		}
		if h, ok := s.hiders[r.SubjectKind]; ok {
			if g.Excerpt, err = h.Excerpt(ctx, r.SubjectID); err != nil {
				return nil, err
			}
		}
		out = append(out, g)
	}
	return out, nil
}

// Decide is an operator's call on a reported subject. hide and unhide go
// through the owning slice and on the record; every decision closes the
// subject's open reports. Non-operators are not found.
func (s *Service) Decide(ctx context.Context, operatorID uuid.UUID, kind string, subjectID uuid.UUID, action string) error {
	if !s.IsOperator(operatorID) {
		return apperr.ErrNotFound
	}
	h, ok := s.hiders[kind]
	if !ok {
		return apperr.ErrNotFound
	}
	status := "dismissed"
	switch action {
	case DecideHide:
		if err := h.Hide(ctx, subjectID); err != nil {
			return err
		}
		status = "actioned"
	case DecideUnhide:
		if err := h.Unhide(ctx, subjectID); err != nil {
			return err
		}
	case DecideDismiss:
	default:
		return apperr.FieldErrors{}.Add("action", "Choose hide, unhide or dismiss.")
	}
	return s.inTx(ctx, "decide", func(q *moderationdb.Queries) error {
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

// ReportedBy says which of ids reporterID has reported, so a list can drop
// them for that person straight away.
func (s *Service) ReportedBy(ctx context.Context, reporterID uuid.UUID, kind string, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	rows, err := s.q.ReportedBy(ctx, moderationdb.ReportedByParams{ReporterID: reporterID, SubjectKind: kind, Ids: ids})
	if err != nil {
		return nil, apperr.Wrap(err, "reported by")
	}
	out := make(map[uuid.UUID]bool, len(rows))
	for _, id := range rows {
		out[id] = true
	}
	return out, nil
}

func (s *Service) TermsAccepted(ctx context.Context, userID uuid.UUID) (bool, error) {
	ok, err := s.q.TermsAccepted(ctx, moderationdb.TermsAcceptedParams{UserID: userID, Version: TermsVersion})
	if err != nil {
		return false, apperr.Wrap(err, "terms accepted")
	}
	return ok, nil
}

func (s *Service) AcceptTerms(ctx context.Context, userID uuid.UUID) error {
	if err := s.q.AcceptTerms(ctx, moderationdb.AcceptTermsParams{UserID: userID, Version: TermsVersion}); err != nil {
		return apperr.Wrap(err, "accept terms")
	}
	return nil
}

// Suspend stops userID posting until until, or until lifted when until is
// nil. actorID is nil for an automatic suspension.
func (s *Service) Suspend(ctx context.Context, actorID *uuid.UUID, userID uuid.UUID, until *time.Time, reason string) error {
	return s.inTx(ctx, "suspend", func(q *moderationdb.Queries) error {
		if err := q.Suspend(ctx, moderationdb.SuspendParams{UserID: userID, Until: until, Reason: reason}); err != nil {
			return err
		}
		return q.InsertAction(ctx, moderationdb.InsertActionParams{
			ActorID: actorID, SubjectKind: "user", SubjectID: userID, Action: "suspend", Reason: reason,
		})
	})
}

// Unsuspend lifts a suspension; lifting none is a no-op.
func (s *Service) Unsuspend(ctx context.Context, actorID *uuid.UUID, userID uuid.UUID) error {
	return s.inTx(ctx, "unsuspend", func(q *moderationdb.Queries) error {
		n, err := q.Unsuspend(ctx, userID)
		if err != nil || n == 0 {
			return err
		}
		return q.InsertAction(ctx, moderationdb.InsertActionParams{
			ActorID: actorID, SubjectKind: "user", SubjectID: userID, Action: "unsuspend",
		})
	})
}

func (s *Service) Suspended(ctx context.Context, userID uuid.UUID, now time.Time) (bool, error) {
	ok, err := s.q.Suspended(ctx, moderationdb.SuspendedParams{UserID: userID, Now: now})
	if err != nil {
		return false, apperr.Wrap(err, "suspended")
	}
	return ok, nil
}

// CanPost is nil when userID may publish: the terms are accepted and no
// suspension is running.
func (s *Service) CanPost(ctx context.Context, userID uuid.UUID) error {
	accepted, err := s.TermsAccepted(ctx, userID)
	if err != nil {
		return err
	}
	if !accepted {
		return ErrTermsRequired
	}
	suspended, err := s.Suspended(ctx, userID, s.now())
	if err != nil {
		return err
	}
	if suspended {
		return ErrSuspended
	}
	return nil
}

func (s *Service) inTx(ctx context.Context, what string, fn func(*moderationdb.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return apperr.Wrap(err, "begin %s", what)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = fn(s.q.WithTx(tx)); err != nil {
		return apperr.Wrap(err, "%s", what)
	}
	if err = tx.Commit(ctx); err != nil {
		return apperr.Wrap(err, "commit %s", what)
	}
	return nil
}
