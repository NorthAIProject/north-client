-- name: UpsertNewsTickerItem :exec
INSERT INTO news_ticker_items (item_key, title, url, source, feed_url, published_at, fetched_at)
VALUES ($1, $2, $3, $4, $5, $6, now())
ON CONFLICT (item_key) DO UPDATE
SET title = EXCLUDED.title, url = EXCLUDED.url, source = EXCLUDED.source,
    feed_url = EXCLUDED.feed_url, published_at = EXCLUDED.published_at, fetched_at = now();

-- name: ListNewsTickerItems :many
SELECT * FROM news_ticker_items
ORDER BY published_at DESC, id DESC
LIMIT $1;

-- name: PruneNewsTickerItemsOlderThan :exec
DELETE FROM news_ticker_items WHERE published_at < $1;

-- name: PruneNewsTickerItemsBeyond :exec
DELETE FROM news_ticker_items
WHERE id NOT IN (SELECT id FROM news_ticker_items ORDER BY published_at DESC, id DESC LIMIT $1);

-- name: ListUserNewsFeeds :many
SELECT * FROM user_news_feeds WHERE user_id = $1 ORDER BY created_at, id;

-- name: CountUserNewsFeeds :one
SELECT count(*) FROM user_news_feeds WHERE user_id = $1;

-- name: CreateUserNewsFeed :one
INSERT INTO user_news_feeds (user_id, feed_url, title) VALUES ($1, $2, $3) RETURNING *;

-- name: DeleteUserNewsFeed :execrows
DELETE FROM user_news_feeds WHERE id = $1 AND user_id = $2;

-- name: ListDistinctUserNewsFeedURLs :many
SELECT DISTINCT feed_url FROM user_news_feeds ORDER BY feed_url;
