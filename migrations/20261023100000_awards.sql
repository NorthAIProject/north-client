-- +goose Up
-- +goose StatementBegin

-- Badges are achievements rows of kind badge_earned, in a fifth category with
-- a switch of its own, like xp but on by default.
ALTER TABLE achievements DROP CONSTRAINT achievements_category_check;
ALTER TABLE achievements ADD CONSTRAINT achievements_category_check
    CHECK (category IN ('training', 'streaks', 'goals', 'awards'));
ALTER TABLE achievement_sharing DROP CONSTRAINT achievement_sharing_category_check;
ALTER TABLE achievement_sharing ADD CONSTRAINT achievement_sharing_category_check
    CHECK (category IN ('training', 'streaks', 'goals', 'xp', 'awards'));

-- A private badge (nutrition, hydration, sleep, mind, body) is the owner's
-- alone: no feed, profile, kudos or recap reads it for anyone else.
ALTER TABLE achievements ADD COLUMN private boolean NOT NULL DEFAULT false;

-- When the person was shown the badge. NULL = not yet celebrated; only
-- badge_earned rows are ever read for it.
ALTER TABLE achievements ADD COLUMN celebrated_at timestamptz;

-- Awards are shared unless the person says otherwise.
CREATE OR REPLACE FUNCTION sharing_on(p_user uuid, p_category text) RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT COALESCE(
        (SELECT s.shared FROM achievement_sharing s
          WHERE s.user_id = p_user AND s.category = p_category),
        p_category IN ('training', 'streaks', 'goals', 'awards'))
$$;

-- How many people hold each badge, recomputed on a schedule rather than read
-- live. computed_at says how stale it is.
CREATE TABLE badge_rarity (
    badge_id    text PRIMARY KEY,
    earned      int NOT NULL,
    active      int NOT NULL,
    computed_at timestamptz NOT NULL
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE badge_rarity;

CREATE OR REPLACE FUNCTION sharing_on(p_user uuid, p_category text) RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT COALESCE(
        (SELECT s.shared FROM achievement_sharing s
          WHERE s.user_id = p_user AND s.category = p_category),
        p_category IN ('training', 'streaks', 'goals'))
$$;

ALTER TABLE achievements DROP COLUMN celebrated_at;
ALTER TABLE achievements DROP COLUMN private;

DELETE FROM achievement_sharing WHERE category = 'awards';
ALTER TABLE achievement_sharing DROP CONSTRAINT achievement_sharing_category_check;
ALTER TABLE achievement_sharing ADD CONSTRAINT achievement_sharing_category_check
    CHECK (category IN ('training', 'streaks', 'goals', 'xp'));

DELETE FROM achievements WHERE category = 'awards';
ALTER TABLE achievements DROP CONSTRAINT achievements_category_check;
ALTER TABLE achievements ADD CONSTRAINT achievements_category_check
    CHECK (category IN ('training', 'streaks', 'goals'));
-- +goose StatementEnd
