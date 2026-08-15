package health

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

// Ingest records a payload of readings for one provider.
//
// Source is a parameter rather than a field on Reading because it describes the
// whole delivery, not each measurement: one POST is one provider's readings.
// The HTTP surface takes it from the URL path, so a single token can push from
// a phone bridge and a laptop script without the two landing in the same
// bucket — and so a person can later disconnect one without losing the other.
func (s *Service) Ingest(ctx context.Context, userID uuid.UUID, source string, readings []Reading) (Result, error) {
	if err := validate(source, readings); err != nil {
		return Result{}, err
	}

	written, err := s.repo.Save(ctx, userID, source, readings)
	if err != nil {
		return Result{}, err
	}
	return Result{Written: written}, nil
}

// Between returns one metric's readings inside a half-open window, newest
// first.
func (s *Service) Between(ctx context.Context, userID uuid.UUID, metric string, since, until time.Time) ([]Stored, error) {
	return s.repo.Between(ctx, userID, metric, since, until)
}

// Forget removes everything one provider ever wrote for this person.
//
// Memory is a product feature and North does not discard history on its own —
// but a person disconnecting a provider is the explicit request that the
// memory rules make room for.
func (s *Service) Forget(ctx context.Context, userID uuid.UUID, source string) error {
	return s.repo.DeleteBySource(ctx, userID, source)
}
