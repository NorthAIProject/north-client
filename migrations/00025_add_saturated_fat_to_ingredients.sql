-- +goose Up
-- +goose StatementBegin

-- The seed in 00026 carries saturated fat, and it is the one nutrient in that
-- data a coach would actually say something about. Dropping it to fit the
-- existing columns would discard real data to avoid a one-line migration.
--
-- Defaults to 0 rather than NULL, matching every other nutrient column here:
-- "we do not know" and "none" are not distinguished anywhere else in this
-- table, and introducing the distinction for one column would mean every
-- caller has to handle it.
ALTER TABLE ingredients
    ADD COLUMN saturated_fat_g_per_100g double precision NOT NULL DEFAULT 0;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE ingredients DROP COLUMN saturated_fat_g_per_100g;
-- +goose StatementEnd
