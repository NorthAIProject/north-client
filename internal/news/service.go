package news

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/NorthAIProject/north-client/internal/preferences"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Preferences is the slice of the preferences service the ticker needs.
type Preferences interface {
	Get(ctx context.Context, userID uuid.UUID) (preferences.Preferences, error)
	SetNewsTickerEnabled(ctx context.Context, userID uuid.UUID, enabled bool) error
}

// Retention for the headline table. Three days is plenty for a "breaking"
// strip and keeps the sweep's prune cheap.
const (
	keepFor   = 3 * 24 * time.Hour
	keepItems = 300
	sweepPull = 120
)

type Service struct {
	repo    *Repository
	agg     Aggregator
	prefs   Preferences
	curated []string

	// MaxUserFeeds caps what one person can follow. Ten is generous for a
	// strip that shows one headline at a time.
	MaxUserFeeds int
}

func NewService(repo *Repository, agg Aggregator, prefs Preferences, curated []string) *Service {
	return &Service{repo: repo, agg: agg, prefs: prefs, curated: curated, MaxUserFeeds: 10}
}

// Ticker is the strip for one person: nothing when they switched it off.
func (s *Service) Ticker(ctx context.Context, user users.User, limit int) (Ticker, error) {
	p, err := s.prefs.Get(ctx, user.ID)
	if err != nil {
		return Ticker{}, err
	}
	if !p.NewsTickerEnabled {
		return Ticker{Enabled: false}, nil
	}
	items, err := s.repo.ListRecent(ctx, limit)
	if err != nil {
		return Ticker{}, err
	}
	return Ticker{Enabled: true, Items: items}, nil
}

// SetEnabled is the per-user switch.
func (s *Service) SetEnabled(ctx context.Context, user users.User, enabled bool) error {
	return s.prefs.SetNewsTickerEnabled(ctx, user.ID, enabled)
}

// Enabled reads the switch.
func (s *Service) Enabled(ctx context.Context, user users.User) (bool, error) {
	p, err := s.prefs.Get(ctx, user.ID)
	if err != nil {
		return false, err
	}
	return p.NewsTickerEnabled, nil
}

func (s *Service) Feeds(ctx context.Context, user users.User) ([]UserFeed, error) {
	return s.repo.ListUserFeeds(ctx, user.ID)
}

// AddFeed follows a site or feed URL. The aggregator finds the feed behind a
// site address; the first candidate wins.
func (s *Service) AddFeed(ctx context.Context, user users.User, raw string) (UserFeed, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return UserFeed{}, apperr.FieldErrors{}.Add("url", "Enter a full web address, starting with https://.").OrNil()
	}
	n, err := s.repo.CountUserFeeds(ctx, user.ID)
	if err != nil {
		return UserFeed{}, err
	}
	if n >= s.MaxUserFeeds {
		return UserFeed{}, apperr.FieldErrors{}.Add("url", "You are following the maximum number of feeds. Remove one first.").OrNil()
	}
	candidates, err := s.agg.Discover(ctx, raw)
	if err != nil {
		return UserFeed{}, apperr.FieldErrors{}.Add("url", "That address cannot be used as a feed.").OrNil()
	}
	if len(candidates) == 0 {
		return UserFeed{}, apperr.FieldErrors{}.Add("url", "No feed found at that address.").OrNil()
	}
	chosen := candidates[0]
	feed, err := s.repo.CreateUserFeed(ctx, user.ID, chosen.URL, strings.TrimSpace(chosen.Title))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return UserFeed{}, apperr.FieldErrors{}.Add("url", "You already follow that feed.").OrNil()
		}
		return UserFeed{}, err
	}
	return feed, nil
}

func (s *Service) RemoveFeed(ctx context.Context, user users.User, id uuid.UUID) error {
	return s.repo.DeleteUserFeed(ctx, id, user.ID)
}

// Sweep pulls the latest headlines for the curated list plus every feed
// anyone follows, in aggregator-sized batches, and prunes. Idempotent: the
// upsert is keyed on the aggregator's item id.
func (s *Service) Sweep(ctx context.Context) error {
	userFeeds, err := s.repo.AllUserFeedURLs(ctx)
	if err != nil {
		return err
	}
	feeds := mergeFeeds(s.curated, userFeeds)
	for start := 0; start < len(feeds); start += maxFeedsPerCall {
		end := min(start+maxFeedsPerCall, len(feeds))
		doc, err := s.agg.Items(ctx, feeds[start:end], sweepPull)
		if err != nil {
			return err
		}
		if err := s.repo.UpsertItems(ctx, doc.Items); err != nil {
			return err
		}
	}
	return s.repo.Prune(ctx, time.Now().Add(-keepFor), keepItems)
}

// mergeFeeds is curated first, then user feeds, deduped, order preserved.
func mergeFeeds(curated, user []string) []string {
	seen := make(map[string]bool, len(curated)+len(user))
	out := make([]string, 0, len(curated)+len(user))
	for _, u := range append(append([]string{}, curated...), user...) {
		if u == "" || seen[u] {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	return out
}
