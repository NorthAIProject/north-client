-- +goose Up
-- +goose StatementBegin

-- Crews: a few people keeping each other going. A crew of two is an
-- accountability partner. Members see each other's crew signals (checked in
-- today, trained today, streak, the week's challenge) and nothing more;
-- joining is agreeing to that, and the join page says so.
CREATE TABLE crews (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 40),
    owner_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code        text NOT NULL UNIQUE CHECK (code ~ '^[a-z0-9]{10}$'),
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE crew_members (
    crew_id    uuid NOT NULL REFERENCES crews(id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    joined_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (crew_id, user_id)
);
CREATE INDEX crew_members_user ON crew_members (user_id);

-- One weekly challenge per crew: so many check-ins, or so many workouts,
-- each member, Monday to Sunday in their own time zone.
CREATE TABLE crew_challenges (
    crew_id     uuid PRIMARY KEY REFERENCES crews(id) ON DELETE CASCADE,
    kind        text NOT NULL CHECK (kind IN ('checkins', 'workouts')),
    target      smallint NOT NULL CHECK (target BETWEEN 1 AND 7),
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE crew_challenges;
DROP TABLE crew_members;
DROP TABLE crews;
-- +goose StatementEnd
