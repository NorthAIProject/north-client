-- +goose Up
-- +goose StatementBegin

-- The breaking-news ticker. Headlines come from the cluster's shared feed
-- aggregator (platform/infra/docs/feeds.md); the worker sweeps them into this
-- table every five minutes so the dashboard never waits on a network call.
-- Headline, source, time and link only — no bodies, by design: the ticker
-- links out to the publisher rather than republishing.
CREATE TABLE news_ticker_items (
    id           uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    -- The aggregator's stable item id (guid / atom id / url hash). Unique so a
    -- sweep is an idempotent upsert, not a growing pile of duplicates.
    item_key     text        NOT NULL UNIQUE,
    title        text        NOT NULL,
    url          text        NOT NULL,
    source       text        NOT NULL DEFAULT '',
    feed_url     text        NOT NULL,
    published_at timestamptz NOT NULL,
    fetched_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX news_ticker_items_published_idx ON news_ticker_items (published_at DESC);

-- Feeds a person follows on top of the curated list. Private to the user; the
-- aggregator only ever sees the URL.
CREATE TABLE user_news_feeds (
    id         uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    feed_url   text        NOT NULL,
    title      text        NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, feed_url)
);

-- Per-user switch. Default on: the strip is ambient and one tap removes it.
ALTER TABLE user_preferences ADD COLUMN news_ticker_enabled boolean NOT NULL DEFAULT true;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE user_preferences DROP COLUMN news_ticker_enabled;
DROP TABLE user_news_feeds;
DROP TABLE news_ticker_items;
-- +goose StatementEnd
