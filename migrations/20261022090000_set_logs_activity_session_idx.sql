-- +goose Up
-- +goose StatementBegin

-- ListSetsForSessions looks sets up by the session they belong to; the existing
-- set_logs indexes all lead with user_id, so that query scanned the table.
CREATE INDEX set_logs_activity_session_idx ON set_logs (activity_session_id)
 WHERE activity_session_id IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS set_logs_activity_session_idx;
-- +goose StatementEnd
