package nudges

import (
	"context"

	"github.com/google/uuid"
)

const listDefault = 20

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// CreateIfAbsent stores the draft when the dedupe key is new.
// created is false when the same (user, kind, key) already exists.
func (s *Service) CreateIfAbsent(ctx context.Context, userID uuid.UUID, d Draft) (Nudge, bool, error) {
	return s.repo.Insert(ctx, userID, d)
}

func (s *Service) ListOpen(ctx context.Context, userID uuid.UUID, limit int) ([]Nudge, error) {
	if limit <= 0 || limit > 100 {
		limit = listDefault
	}
	return s.repo.ListOpen(ctx, userID, limit)
}

func (s *Service) CountUnread(ctx context.Context, userID uuid.UUID) (int, error) {
	return s.repo.CountUnread(ctx, userID)
}

func (s *Service) MarkRead(ctx context.Context, id, userID uuid.UUID) (Nudge, error) {
	return s.repo.MarkRead(ctx, id, userID)
}

func (s *Service) Dismiss(ctx context.Context, id, userID uuid.UUID) (Nudge, error) {
	return s.repo.Dismiss(ctx, id, userID)
}
