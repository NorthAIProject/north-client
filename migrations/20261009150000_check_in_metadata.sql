-- +goose Up
-- +goose StatementBegin

-- Where a check-in came from and a little more of how the day felt.
--
-- source is an open label ("web", "ios", "siri", "coach", "mcp", "capture"),
-- not an enum: a new surface must not need a migration, and rows written before
-- this column existed honestly say "unknown" rather than guessing.
-- stress and sleep_quality are optional 1–5 scales like mood and energy; NULL
-- means not given. tags are short lowercase labels, normalised by the service.
ALTER TABLE check_ins
    ADD COLUMN source        text     NOT NULL DEFAULT 'unknown',
    ADD COLUMN stress        smallint CHECK (stress IS NULL OR stress BETWEEN 1 AND 5),
    ADD COLUMN sleep_quality smallint CHECK (sleep_quality IS NULL OR sleep_quality BETWEEN 1 AND 5),
    ADD COLUMN tags          text[]   NOT NULL DEFAULT '{}';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE check_ins
    DROP COLUMN tags,
    DROP COLUMN sleep_quality,
    DROP COLUMN stress,
    DROP COLUMN source;
-- +goose StatementEnd
