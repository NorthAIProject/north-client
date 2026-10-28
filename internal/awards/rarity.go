package awards

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/NorthAIProject/north-client/internal/awards/award"
)

// activeWindow is how recently someone must have logged to count as active.
const activeWindow = 90 * 24 * time.Hour

// Rarity is how common a badge is among people who still use the app.
type Rarity struct {
	BadgeID string
	Earned  int
	Active  int
}

// WithHolder is the rarity of a badge the viewer holds: the daily recount may
// predate their grant, but a badge they hold has at least one holder.
func (r Rarity) WithHolder() Rarity {
	r.Earned = max(r.Earned, 1)
	r.Active = max(r.Active, 1)
	return r
}

// Label is the share of active people who hold the badge, as the shelf shows
// it: empty when nobody is active or nobody holds it (nothing to say, never
// "0%"), "<1%" below one percent, otherwise a whole percent.
func (r Rarity) Label() string {
	if r.Active <= 0 || r.Earned <= 0 {
		return ""
	}
	pct := float64(r.Earned) * 100 / float64(r.Active)
	if pct > 100 {
		pct = 100
	}
	if pct > 0 && pct < 1 {
		return "<1%"
	}
	return fmt.Sprintf("%d%%", int(math.Round(pct)))
}

// Rarities is the last computed rarity of every badge that has one.
func (s *Service) Rarities(ctx context.Context) (map[string]Rarity, error) {
	return s.repo.Rarities(ctx)
}

// ComputeRarity recounts, at now, who is active and who holds each badge, and
// stores one row for every badge in the catalog. It is a count over the whole
// table, so it runs daily rather than per request.
func (s *Service) ComputeRarity(ctx context.Context, now time.Time) error {
	active, err := s.repo.CountActiveUsers(ctx, now.Add(-activeWindow))
	if err != nil {
		return fmt.Errorf("awards: count active: %w", err)
	}
	earned, err := s.repo.CountBadgeEarners(ctx)
	if err != nil {
		return fmt.Errorf("awards: count earners: %w", err)
	}
	for _, b := range award.Catalog() {
		if err := s.repo.UpsertRarity(ctx, Rarity{BadgeID: b.ID, Earned: earned[b.ID], Active: active}, now); err != nil {
			return fmt.Errorf("awards: store rarity %s: %w", b.ID, err)
		}
	}
	return nil
}

// HandleRarity is the job handler for KindSweepRarity.
func (s *Service) HandleRarity(ctx context.Context, _ json.RawMessage) error {
	return s.ComputeRarity(ctx, time.Now())
}
