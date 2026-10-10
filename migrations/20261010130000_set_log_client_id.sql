-- +goose Up
-- +goose StatementBegin

-- The id a client gave a set before it was sent, so a retry after a lost
-- response finds the set it already logged instead of logging it again. NULL
-- for every set logged without one — the web form, the coach, every set before
-- this — and NULLs never collide in a unique index, so those keep logging a new
-- row each time. Scoped to the account: it is the client's id, not ours.
ALTER TABLE set_logs ADD COLUMN client_id uuid;

CREATE UNIQUE INDEX set_logs_user_client_id_key ON set_logs (user_id, client_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX set_logs_user_client_id_key;
ALTER TABLE set_logs DROP COLUMN client_id;
-- +goose StatementEnd
