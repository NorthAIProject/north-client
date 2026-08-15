package health

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type Service struct {
	repo *Repository

	// Set by WithWorkouts. Nil is the ordinary state for a deployment that
	// only accepts readings.
	activities ActivityImporter
	biometrics BiometricsLookup
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

// Summary renders the headline metrics of the last `days` days as sentences,
// newest window first.
//
// Metrics with no readings in the window produce no line at all. Saying
// "Resting heart rate — no data" for every metric a person does not track
// would fill the coach's context with absences, and an absence is not a
// signal: most people wear one device measuring three things.
func (s *Service) Summary(ctx context.Context, userID uuid.UUID, now time.Time, days int) ([]string, error) {
	if days <= 0 {
		days = defaultSummaryDays
	}
	since := now.AddDate(0, 0, -days)

	lines := make([]string, 0, len(headlines))
	for _, h := range headlines {
		stats, err := s.repo.Stats(ctx, userID, h.metric, since, now)
		if err != nil {
			return nil, err
		}
		if line, ok := h.describe(stats, days); ok {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

// defaultSummaryDays is the window the coach reads.
//
// A week: long enough that one bad night does not read as a trend, short
// enough that it still describes now rather than last month.
const defaultSummaryDays = 7

// Forget removes everything one provider ever wrote for this person.
//
// Memory is a product feature and North does not discard history on its own —
// but a person disconnecting a provider is the explicit request that the
// memory rules make room for.
func (s *Service) Forget(ctx context.Context, userID uuid.UUID, source string) error {
	return s.repo.DeleteBySource(ctx, userID, source)
}
