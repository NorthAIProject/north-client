-- +goose Up
-- Light, dark, or follow the device: how this person wants Khepri to look.
--
-- NULL means "never chosen", and it is deliberately not defaulted. Each client
-- applies its own default to a NULL: the web renders dark until light mode
-- passes its audit, iOS follows the system. A column default would bake one
-- client's answer into every row, and flipping the web to "system" later would
-- then need a data migration instead of a one-constant change.
ALTER TABLE users
  ADD COLUMN appearance text NULL
    CONSTRAINT users_appearance_check CHECK (appearance IN ('system', 'light', 'dark'));

COMMENT ON COLUMN users.appearance IS
  'Chosen appearance: system, light or dark. NULL until the user picks one; clients apply their own default.';

-- +goose Down
ALTER TABLE users
  DROP COLUMN appearance;
