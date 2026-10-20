-- +goose Up
-- +goose StatementBegin

-- Crews become spaces: channels to talk in, roles to run them, and a
-- visibility for who may find one. 'invite' is the only way in today;
-- 'friends' lets friends of members ask to join; 'open' is reserved and never
-- offered until moderation can carry strangers.
ALTER TABLE crews
    ADD COLUMN visibility  text NOT NULL DEFAULT 'invite' CHECK (visibility IN ('invite', 'friends', 'open')),
    ADD COLUMN description text NOT NULL DEFAULT '' CHECK (char_length(description) <= 200);

-- The owner stays crews.owner_id, the one place ownership lives; a member row
-- is an admin or not. notify and muted_until are the member's own say over
-- pushes from this crew.
ALTER TABLE crew_members
    ADD COLUMN role        text NOT NULL DEFAULT 'member' CHECK (role IN ('admin', 'member')),
    ADD COLUMN notify      text NOT NULL DEFAULT 'mentions' CHECK (notify IN ('all', 'mentions', 'none')),
    ADD COLUMN muted_until timestamptz;

-- #general is every crew's default channel and cannot be deleted; #wins is
-- where the achievements members already share land.
CREATE TABLE crew_channels (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    crew_id     uuid NOT NULL REFERENCES crews(id) ON DELETE CASCADE,
    kind        text NOT NULL CHECK (kind IN ('text', 'board', 'wins')),
    name        text NOT NULL CHECK (name ~ '^[a-z0-9][a-z0-9-]{0,29}$'),
    topic       text NOT NULL DEFAULT '' CHECK (char_length(topic) <= 120),
    position    smallint NOT NULL DEFAULT 0,
    is_default  boolean NOT NULL DEFAULT false,
    created_by  uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (crew_id, name)
);
CREATE UNIQUE INDEX crew_channels_one_wins ON crew_channels (crew_id) WHERE kind = 'wins';
CREATE UNIQUE INDEX crew_channels_one_default ON crew_channels (crew_id) WHERE is_default;

INSERT INTO crew_channels (crew_id, kind, name, is_default, position)
SELECT id, 'text', 'general', true, 0 FROM crews
ON CONFLICT DO NOTHING;

INSERT INTO crew_channels (crew_id, kind, name, is_default, position)
SELECT id, 'wins', 'wins', false, 1 FROM crews
ON CONFLICT DO NOTHING;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE crew_channels;
ALTER TABLE crew_members DROP COLUMN muted_until, DROP COLUMN notify, DROP COLUMN role;
ALTER TABLE crews DROP COLUMN description, DROP COLUMN visibility;
-- +goose StatementEnd
