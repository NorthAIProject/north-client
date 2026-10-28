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

	"github.com/NorthAIProject/north-client/internal/awards/award"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Earned is a badge a person holds.
type Earned struct {
	AchievementID uuid.UUID
	Badge         award.Badge
	At            time.Time
}

type Service struct {
	repo Repository
	log  *slog.Logger
	// failMetric makes one metric error; set only by tests.
	failMetric metric
	// now is the clock; tests replace it.
	now func() time.Time
}

// Repository is the persistence Service depends on: the badges people hold,
// the history the rules count, the monthly challenges and the rarity
// recount. NewRepository is what the process wires, and a test can hand
// Service anything that keeps them.
type Repository interface {
	Timezone(ctx context.Context, userID uuid.UUID) (string, error)
	Count(ctx context.Context, userID uuid.UUID, m metric, tz string, maxGap int) (int, error)
	ProgressCadence(ctx context.Context, userID uuid.UUID) (string, error)
	WorkSets(ctx context.Context, userID uuid.UUID) ([]lift.Set, error)
	Badges(ctx context.Context, userID uuid.UUID) ([]heldBadge, error)
	InsertBadge(ctx context.Context, userID uuid.UUID, b award.Badge, at time.Time) (uuid.UUID, bool, error)
	MarkBadgeSeen(ctx context.Context, userID uuid.UUID, badgeID string) error
	ActiveAreas(ctx context.Context, userID uuid.UUID, since time.Time) ([]award.Area, error)
	MonthlyChallenge(ctx context.Context, userID uuid.UUID, start time.Time) (storedMonthly, error)
	CreateMonthlyChallenge(ctx context.Context, userID uuid.UUID, start time.Time, m MonthlyMetric, target int) error
	MonthlyHistory(ctx context.Context, userID uuid.UUID, start time.Time, tz string) (monthlyHistory, error)
	MonthlyProgress(ctx context.Context, userID uuid.UUID, tz string, start time.Time, m MonthlyMetric) (int, error)
	MonthlyMetAt(ctx context.Context, userID uuid.UUID, key string) (time.Time, error)
	InsertMonthlyMet(ctx context.Context, userID uuid.UUID, key string, at time.Time) error
	MarkMonthlySeen(ctx context.Context, userID uuid.UUID, key string) error
	MonthlyMedals(ctx context.Context, userID uuid.UUID) ([]MonthlyMedal, error)
	UnseenMonthlyMedals(ctx context.Context, userID uuid.UUID) ([]MonthlyMedal, error)
	Rarities(ctx context.Context) (map[string]Rarity, error)
	CountActiveUsers(ctx context.Context, since time.Time) (int, error)
	CountBadgeEarners(ctx context.Context) (map[string]int, error)
	UpsertRarity(ctx context.Context, r Rarity, at time.Time) error
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo, log: slog.Default(), now: time.Now}
}

// isNotFound reports a row the repository did not find.
func isNotFound(err error) bool { return errors.Is(err, apperr.ErrNotFound) }

// Check runs every rule of the areas and records each badge newly met. It is
// best effort: an error is logged, never returned, because a badge is a side
// effect of the write that triggered it and that write must not fail for it.
func (s *Service) Check(ctx context.Context, userID uuid.UUID, at time.Time, areas ...award.Area) {
	if _, err := s.Evaluate(ctx, userID, at, areas...); err != nil {
		s.log.Warn("could not check awards", "user_id", userID, "error", err)
	}
	if monthlyAreas(areas) {
		s.checkMonthly(ctx, userID, s.now())
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
	c := newCounter(ctx, s.repo, userID)
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
	c := newCounter(ctx, s.repo, userID)
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
		if held[b.ID] {
			// Done: nothing to hint, and the count may be a costly walk.
			out[b.ID] = b.Target
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
	if err := s.repo.MarkBadgeSeen(ctx, userID, badgeID); err != nil {
		return fmt.Errorf("awards: mark seen: %w", err)
	}
	return nil
}

func (s *Service) list(ctx context.Context, userID uuid.UUID, unseenOnly bool) ([]Earned, error) {
	rows, err := s.repo.Badges(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("awards: list: %w", err)
	}
	out := make([]Earned, 0, len(rows))
	for _, r := range rows {
		b, ok := award.ByID(r.BadgeID)
		if !ok || (unseenOnly && r.Celebrated) {
			continue
		}
		out = append(out, Earned{AchievementID: r.AchievementID, Badge: b, At: r.At})
	}
	return out, nil
}

func (s *Service) held(ctx context.Context, userID uuid.UUID) (map[string]bool, error) {
	rows, err := s.repo.Badges(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("awards: held: %w", err)
	}
	held := make(map[string]bool, len(rows))
	for _, r := range rows {
		held[r.BadgeID] = true
	}
	return held, nil
}

// insert stores a badge once. ok is false when the person already had it.
func (s *Service) insert(ctx context.Context, userID uuid.UUID, b award.Badge, at time.Time) (Earned, bool, error) {
	id, inserted, err := s.repo.InsertBadge(ctx, userID, b, at)
	if err != nil {
		return Earned{}, false, fmt.Errorf("awards: insert %s: %w", b.ID, err)
	}
	if !inserted {
		return Earned{}, false, nil
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
