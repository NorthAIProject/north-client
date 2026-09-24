-- +goose Up
-- +goose StatementBegin

-- Pending Strava connections started from the iOS app.
--
-- The web flow keeps its OAuth state in a cookie and finds the person from
-- their session cookie at the callback. The app's sign-in sheet has neither:
-- it is a separate browser context, and the app proves itself with a bearer
-- token the browser never sees. So the app asks the server to begin, the state
-- is stored here against the person, and the callback finds them by the state
-- alone.
--
-- Only a hash is kept, so a read of this table cannot be replayed as a live
-- state. Each row is taken once and lives ten minutes.
CREATE TABLE strava_oauth_states (
    state_hash  bytea       PRIMARY KEY,
    user_id     uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at  timestamptz NOT NULL
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE strava_oauth_states;
-- +goose StatementEnd
