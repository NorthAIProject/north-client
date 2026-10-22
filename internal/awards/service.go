// Package awards decides when a person has earned a badge.
//
// A badge is earned on the next qualifying event, never in bulk: rules count a
// person's whole history, but they run only when something calls Check or
// Evaluate after a successful write. Nothing here runs at deploy.
package awards

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/awards/award"
	awardsdb "github.com/NorthAIProject/north-client/internal/awards/db"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
)

// Earned is a badge a person holds.
type Earned struct {
	AchievementID uuid.UUID
	Badge         award.Badge
	At            time.Time
}

type Service struct {
	q   *awardsdb.Queries
	log *slog.Logger
	// failMetric makes one metric error; set only by tests.
	failMetric metric
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{q: awardsdb.New(pool), log: slog.Default()}
}

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// Check runs every rule of the areas and records each badge newly met. It is
// best effort: an error is logged, never returned, because a badge is a side
// effect of the write that triggered it and that write must not fail for it.
func (s *Service) Check(ctx context.Context, userID uuid.UUID, at time.Time, areas ...award.Area) {
	if _, err := s.Evaluate(ctx, userID, at, areas...); err != nil {
		s.log.Warn("could not check awards", "user_id", userID, "error", err)
	}
}

// Evaluate is Check that reports what it earned. Every badge met in one call
// shares at, which is how they are shown as one moment. With no areas it runs
// them all.
func (s *Service) Evaluate(ctx context.Context, userID uuid.UUID, at time.Time, areas ...award.Area) ([]Earned, error) {
	held, err := s.held(ctx, userID)
	if err != nil {
		return nil, err
	}
	c := newCounter(ctx, s.q, userID)
	c.fail = s.failMetric
	var out []Earned
	var errs []error
	for _, b := range award.Catalog() {
		if !inAreas(b.Area, areas) || held[b.ID] {
			continue
		}
		if _, ok := ruleOf[b.ID]; !ok {
			continue
		}
		// Each rule stands alone: one failing query must not cost the
		// person the other badges this event earned.
		n, err := c.progressOf(b)
		if err == nil && n >= b.Target {
			var e Earned
			var ok bool
			if e, ok, err = s.insert(ctx, userID, b, at); ok {
				out = append(out, e)
			}
		}
		if err != nil {
			s.log.Warn("awards rule failed", "badge", b.ID, "user_id", userID, "error", err)
			errs = append(errs, fmt.Errorf("%s: %w", b.ID, err))
		}
	}
	return out, errors.Join(errs...)
}

// Grant records a badge that has no counting rule, such as listened_to_body,
// when the event that earns it happens. Best effort, and once only.
func (s *Service) Grant(ctx context.Context, userID uuid.UUID, at time.Time, badgeID string) {
	b, ok := award.ByID(badgeID)
	if !ok {
		s.log.Warn("could not grant unknown badge", "badge", badgeID, "user_id", userID)
		return
	}
	if _, _, err := s.insert(ctx, userID, b, at); err != nil {
		s.log.Warn("could not grant badge", "badge", badgeID, "user_id", userID, "error", err)
	}
}

// Progress is the current count toward every badge, for the hints on locked
// ones. A badge with no counting rule reads 1 once earned and 0 before.
func (s *Service) Progress(ctx context.Context, userID uuid.UUID) (map[string]int, error) {
	held, err := s.held(ctx, userID)
	if err != nil {
		return nil, err
	}
	c := newCounter(ctx, s.q, userID)
	out := make(map[string]int, len(award.Catalog()))
	for _, b := range award.Catalog() {
		if _, ok := ruleOf[b.ID]; !ok {
			if held[b.ID] {
				out[b.ID] = 1
			} else {
				out[b.ID] = 0
			}
			continue
		}
		n, err := c.progressOf(b)
		if err != nil {
			return nil, err
		}
		out[b.ID] = n
	}
	return out, nil
}

// Earned is every badge the person holds, oldest first.
func (s *Service) Earned(ctx context.Context, userID uuid.UUID) ([]Earned, error) {
	return s.list(ctx, userID, false)
}

// Unseen is the badges not yet celebrated, oldest first.
func (s *Service) Unseen(ctx context.Context, userID uuid.UUID) ([]Earned, error) {
	return s.list(ctx, userID, true)
}

// MarkSeen records that the person was shown the badge. Safe to repeat.
func (s *Service) MarkSeen(ctx context.Context, userID uuid.UUID, badgeID string) error {
	if err := s.q.MarkBadgeSeen(ctx, awardsdb.MarkBadgeSeenParams{UserID: userID, SourceKey: badgeID}); err != nil {
		return fmt.Errorf("awards: mark seen: %w", err)
	}
	return nil
}

func (s *Service) list(ctx context.Context, userID uuid.UUID, unseenOnly bool) ([]Earned, error) {
	rows, err := s.q.ListBadges(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("awards: list: %w", err)
	}
	out := make([]Earned, 0, len(rows))
	for _, r := range rows {
		b, ok := award.ByID(r.SourceKey)
		if !ok || (unseenOnly && r.CelebratedAt != nil) {
			continue
		}
		out = append(out, Earned{AchievementID: r.ID, Badge: b, At: r.OccurredAt})
	}
	return out, nil
}

func (s *Service) held(ctx context.Context, userID uuid.UUID) (map[string]bool, error) {
	rows, err := s.q.ListBadges(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("awards: held: %w", err)
	}
	held := make(map[string]bool, len(rows))
	for _, r := range rows {
		held[r.SourceKey] = true
	}
	return held, nil
}

// insert stores a badge once. ok is false when the person already had it.
func (s *Service) insert(ctx context.Context, userID uuid.UUID, b award.Badge, at time.Time) (Earned, bool, error) {
	id, err := s.q.InsertBadge(ctx, awardsdb.InsertBadgeParams{
		UserID: userID, Title: i18n.Translate("en", "awards.badge."+b.ID+".name"),
		OccurredAt: at, SourceKey: b.ID, Private: b.Area.Private(),
	})
	if err != nil {
		if isNoRows(err) {
			return Earned{}, false, nil
		}
		return Earned{}, false, fmt.Errorf("awards: insert %s: %w", b.ID, err)
	}
	return Earned{AchievementID: id, Badge: b, At: at}, true, nil
}

func inAreas(a award.Area, areas []award.Area) bool {
	if len(areas) == 0 {
		return true
	}
	for _, x := range areas {
		if x == a {
			return true
		}
	}
	return false
}
