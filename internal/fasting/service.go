package fasting

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/awards/award"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

// DefaultTargetHours is the 16:8 fast most people start with.
const DefaultTargetHours = 16

type Service struct {
	repo   *Repository
	now    func() time.Time
	awards Awards
}

// Awards checks a person's badges after a write. awards.Service satisfies it.
type Awards interface {
	Check(ctx context.Context, userID uuid.UUID, at time.Time, areas ...award.Area)
}

func (s *Service) WithAwards(a Awards) *Service {
	s.awards = a
	return s
}

func NewService(repo *Repository) *Service { return &Service{repo: repo, now: time.Now} }

// Start opens a fast now, or at a remembered time ("I stopped eating at 8").
func (s *Service) Start(ctx context.Context, user users.User, targetHours int, at *time.Time) (Session, error) {
	if targetHours == 0 {
		targetHours = DefaultTargetHours
	}
	if targetHours < 1 || targetHours > 72 {
		return Session{}, apperr.FieldErrors{}.Add("targetHours", "Choose a target between 1 and 72 hours.")
	}
	start := s.now()
	if at != nil {
		if at.After(start) {
			return Session{}, apperr.FieldErrors{}.Add("startedAt", "A fast cannot start in the future.")
		}
		start = *at
	}
	return s.repo.Start(ctx, user.ID, start, targetHours)
}

// Stop ends the open fast now.
func (s *Service) Stop(ctx context.Context, user users.User) (Session, error) {
	stopped, err := s.repo.Stop(ctx, user.ID, s.now())
	if err == nil && s.awards != nil {
		s.awards.Check(ctx, user.ID, s.now(), award.Body)
	}
	return stopped, err
}

// Current is the open fast, if there is one.
func (s *Service) Current(ctx context.Context, user users.User) (Session, bool, error) {
	return s.repo.Open(ctx, user.ID)
}

// Overlapping lists every fast touching a window, newest first. A fast that
// began last night belongs to this morning's view too.
func (s *Service) Overlapping(ctx context.Context, user users.User, rg timerange.Range) ([]Session, error) {
	return s.repo.Overlapping(ctx, user.ID, rg.Since, rg.Until)
}

// Delete removes a fast recorded by mistake.
func (s *Service) Delete(ctx context.Context, user users.User, id uuid.UUID) error {
	return s.repo.Delete(ctx, id, user.ID)
}
