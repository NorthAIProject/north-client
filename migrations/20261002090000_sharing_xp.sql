-- +goose Up
-- +goose StatementBegin

-- A fourth thing a person may share with followers: their XP and level, which
-- is what puts them on a friend's leaderboard. Off unless turned on, like the
-- other three. XP itself is never stored; internal/xp derives it on read.
ALTER TABLE achievement_sharing DROP CONSTRAINT achievement_sharing_category_check;
ALTER TABLE achievement_sharing ADD CONSTRAINT achievement_sharing_category_check
    CHECK (category IN ('training', 'streaks', 'goals', 'xp'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DELETE FROM achievement_sharing WHERE category = 'xp';
ALTER TABLE achievement_sharing DROP CONSTRAINT achievement_sharing_category_check;
ALTER TABLE achievement_sharing ADD CONSTRAINT achievement_sharing_category_check
    CHECK (category IN ('training', 'streaks', 'goals'));
-- +goose StatementEnd
