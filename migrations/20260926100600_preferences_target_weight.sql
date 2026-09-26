-- +goose Up
-- +goose StatementBegin

-- A weight to aim at, for "3.9 kg to goal". A standing setting, so it lives
-- with the other preferences rather than on a measurement row.
ALTER TABLE user_preferences
    ADD COLUMN target_weight_kg double precision CHECK (target_weight_kg BETWEEN 20 AND 400);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE user_preferences DROP COLUMN target_weight_kg;
-- +goose StatementEnd
