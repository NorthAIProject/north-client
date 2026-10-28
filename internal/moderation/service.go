package moderation

import (
	"context"
	"log/slog"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Service struct {
	repo      Repository
	hiders    map[string]Hider
	operators []uuid.UUID
	pushers   []ThreadSender
	log       *slog.Logger
	now       func() time.Time
}

// Repository is the persistence Service depends on: reports, the record of
// moderation actions, accepted terms and suspensions. NewRepository is what
// the process wires, and a test can hand Service anything that keeps them.
type Repository interface {
	FileReport(ctx context.Context, reporterID uuid.UUID, sub Subject, reason, note string,
		overThreshold func(reporters int64) (bool, error), hide func() error) (filed, hidden bool, err error)
	OpenReports(ctx context.Context) ([]ReportGroup, error)
	Decide(ctx context.Context, operatorID uuid.UUID, kind string, subjectID uuid.UUID, action, status string) error
	ReportedBy(ctx context.Context, reporterID uuid.UUID, kind string, ids []uuid.UUID) (map[uuid.UUID]bool, error)
	TermsAccepted(ctx context.Context, userID uuid.UUID) (bool, error)
	AcceptTerms(ctx context.Context, userID uuid.UUID) error
	Suspend(ctx context.Context, actorID *uuid.UUID, userID uuid.UUID, until *time.Time, reason string) error
	Unsuspend(ctx context.Context, actorID *uuid.UUID, userID uuid.UUID) error
	Suspended(ctx context.Context, userID uuid.UUID, now time.Time) (bool, error)
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo, hiders: map[string]Hider{}, log: slog.Default(), now: time.Now}
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

	// Over the slice's threshold, the subject is hidden unless hiding was
	// already decided. The threshold can fall below the count when a crew
	// shrinks, so this is not equality.
	overThreshold := func(reporters int64) (bool, error) {
		threshold, tErr := h.Threshold(ctx, sub.ID)
		if tErr != nil {
			return false, tErr
		}
		return reporters >= int64(threshold), nil
	}
	// The slice hides in its own tables, outside the report's transaction;
	// Hide is idempotent, so a failed commit after it only loses the record.
	hide := func() error { return h.Hide(ctx, sub.ID) }
	filed, hidden, err := s.repo.FileReport(ctx, reporterID, sub, reason, note, overThreshold, hide)
	if err != nil || !filed {
		return false, err
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
	out, err := s.repo.OpenReports(ctx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		g := &out[i]
		if h, ok := s.hiders[g.Kind]; ok {
			if g.Excerpt, err = h.Excerpt(ctx, g.SubjectID); err != nil {
				return nil, err
			}
		}
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
	return s.repo.Decide(ctx, operatorID, kind, subjectID, action, status)
}

// ReportedBy says which of ids reporterID has reported, so a list can drop
// them for that person straight away.
func (s *Service) ReportedBy(ctx context.Context, reporterID uuid.UUID, kind string, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	return s.repo.ReportedBy(ctx, reporterID, kind, ids)
}

func (s *Service) TermsAccepted(ctx context.Context, userID uuid.UUID) (bool, error) {
	return s.repo.TermsAccepted(ctx, userID)
}

func (s *Service) AcceptTerms(ctx context.Context, userID uuid.UUID) error {
	return s.repo.AcceptTerms(ctx, userID)
}

// Suspend stops userID posting until until, or until lifted when until is
// nil. actorID is nil for an automatic suspension.
func (s *Service) Suspend(ctx context.Context, actorID *uuid.UUID, userID uuid.UUID, until *time.Time, reason string) error {
	return s.repo.Suspend(ctx, actorID, userID, until, reason)
}

// Unsuspend lifts a suspension; lifting none is a no-op.
func (s *Service) Unsuspend(ctx context.Context, actorID *uuid.UUID, userID uuid.UUID) error {
	return s.repo.Unsuspend(ctx, actorID, userID)
}

func (s *Service) Suspended(ctx context.Context, userID uuid.UUID, now time.Time) (bool, error) {
	return s.repo.Suspended(ctx, userID, now)
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
