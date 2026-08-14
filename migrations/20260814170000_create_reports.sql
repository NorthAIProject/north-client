-- +goose Up
-- +goose StatementBegin

-- Weekly (and later monthly) reviews the person reads. One active row per
-- user per week: archive frees the slot so a new generate can replace it.
CREATE TABLE reports (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,

    kind          text        NOT NULL DEFAULT 'weekly',
    period_start  date        NOT NULL,
    period_end    date        NOT NULL,

    title         text        NOT NULL,
    body          text        NOT NULL DEFAULT '',
    status        text        NOT NULL DEFAULT 'pending'
                              CHECK (status IN ('pending', 'ready', 'failed')),
    last_error    text        NOT NULL DEFAULT '',

    generated_at  timestamptz,
    archived_at   timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX reports_one_active_week
    ON reports (user_id, kind, period_start)
    WHERE archived_at IS NULL;

CREATE INDEX reports_user_created_idx
    ON reports (user_id, created_at DESC)
    WHERE archived_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE reports;
-- +goose StatementEnd
