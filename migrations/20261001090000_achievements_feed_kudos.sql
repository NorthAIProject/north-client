-- +goose Up
-- +goose StatementBegin

-- Achievements: moments worth showing friends, recorded where they happen —
-- a workout finished, a streak reached, a goal or milestone completed. Not an
-- economy: no points, no levels (docs/advanced-gamification.md gates those).
--
-- Each belongs to a category, and a follower sees it only if its owner shares
-- that category. Nothing else anybody logs is ever in here.
CREATE TABLE achievements (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category     text NOT NULL CHECK (category IN ('training', 'streaks', 'goals')),
    kind         text NOT NULL,
    title        text NOT NULL,
    detail       text NOT NULL DEFAULT '',
    occurred_at  timestamptz NOT NULL,
    -- What this was recorded from (a session, a goal, a streak on a day), so
    -- the same moment is never recorded twice.
    source_key   text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, kind, source_key)
);
CREATE INDEX achievements_user_time ON achievements (user_id, occurred_at DESC);

-- Off unless turned on, category by category.
CREATE TABLE achievement_sharing (
    user_id   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category  text NOT NULL CHECK (category IN ('training', 'streaks', 'goals')),
    shared    boolean NOT NULL DEFAULT false,
    PRIMARY KEY (user_id, category)
);

CREATE TABLE kudos (
    achievement_id  uuid NOT NULL REFERENCES achievements(id) ON DELETE CASCADE,
    user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (achievement_id, user_id)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE kudos;
DROP TABLE achievement_sharing;
DROP TABLE achievements;
-- +goose StatementEnd
