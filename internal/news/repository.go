package news

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	newsdb "github.com/NorthAIProject/north-client/internal/news/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *newsdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: newsdb.New(pool)}
}

// UpsertItems writes headlines keyed by the aggregator's item id.
func (r *Repository) UpsertItems(ctx context.Context, items []Item) error {
	for _, it := range items {
		if err := r.q.UpsertNewsTickerItem(ctx, newsdb.UpsertNewsTickerItemParams{
			ItemKey: it.Key, Title: it.Title, Url: it.URL, Source: it.Source, FeedUrl: it.FeedURL, PublishedAt: it.PublishedAt,
		}); err != nil {
			return apperr.Wrap(err, "upsert news item")
		}
	}
	return nil
}

func (r *Repository) ListRecent(ctx context.Context, limit int) ([]Item, error) {
	rows, err := r.q.ListNewsTickerItems(ctx, int32(limit))
	if err != nil {
		return nil, apperr.Wrap(err, "list news items")
	}
	out := make([]Item, 0, len(rows))
	for _, row := range rows {
		out = append(out, itemFromDB(row))
	}
	return out, nil
}

// Prune drops headlines older than `before` and beyond the newest `keep`.
func (r *Repository) Prune(ctx context.Context, before time.Time, keep int) error {
	if err := r.q.PruneNewsTickerItemsOlderThan(ctx, before); err != nil {
		return apperr.Wrap(err, "prune old news items")
	}
	if err := r.q.PruneNewsTickerItemsBeyond(ctx, int32(keep)); err != nil {
		return apperr.Wrap(err, "prune surplus news items")
	}
	return nil
}

func (r *Repository) ListUserFeeds(ctx context.Context, userID uuid.UUID) ([]UserFeed, error) {
	rows, err := r.q.ListUserNewsFeeds(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "list user news feeds")
	}
	out := make([]UserFeed, 0, len(rows))
	for _, row := range rows {
		out = append(out, feedFromDB(row))
	}
	return out, nil
}

func (r *Repository) CountUserFeeds(ctx context.Context, userID uuid.UUID) (int, error) {
	n, err := r.q.CountUserNewsFeeds(ctx, userID)
	if err != nil {
		return 0, apperr.Wrap(err, "count user news feeds")
	}
	return int(n), nil
}

func (r *Repository) CreateUserFeed(ctx context.Context, userID uuid.UUID, feedURL, title string) (UserFeed, error) {
	row, err := r.q.CreateUserNewsFeed(ctx, newsdb.CreateUserNewsFeedParams{UserID: userID, FeedUrl: feedURL, Title: title})
	if err != nil {
		return UserFeed{}, apperr.Wrap(err, "create user news feed")
	}
	return feedFromDB(row), nil
}

func (r *Repository) DeleteUserFeed(ctx context.Context, id, userID uuid.UUID) error {
	n, err := r.q.DeleteUserNewsFeed(ctx, newsdb.DeleteUserNewsFeedParams{ID: id, UserID: userID})
	if err != nil {
		return apperr.Wrap(err, "delete user news feed")
	}
	if n == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

// AllUserFeedURLs is every distinct feed anyone follows, for the sweep.
func (r *Repository) AllUserFeedURLs(ctx context.Context) ([]string, error) {
	urls, err := r.q.ListDistinctUserNewsFeedURLs(ctx)
	if err != nil {
		return nil, apperr.Wrap(err, "list user news feed urls")
	}
	return urls, nil
}

func itemFromDB(row newsdb.NewsTickerItem) Item {
	return Item{
		ID: row.ID, Key: row.ItemKey, Title: row.Title, URL: row.Url, Source: row.Source,
		FeedURL: row.FeedUrl, PublishedAt: row.PublishedAt, FetchedAt: row.FetchedAt,
	}
}

func feedFromDB(row newsdb.UserNewsFeed) UserFeed {
	return UserFeed{ID: row.ID, UserID: row.UserID, URL: row.FeedUrl, Title: row.Title, CreatedAt: row.CreatedAt}
}
