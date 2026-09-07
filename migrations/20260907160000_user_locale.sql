-- +goose Up
-- The language Khepri speaks to this user in.
--
-- Separate from timezone even though both are "where you are": a Brazilian in
-- Lisbon wants Europe/Lisbon and pt-BR, and inferring one from the other would
-- get that person wrong every time.
--
-- pt-PT and pt-BR are stored apart rather than folded into one "pt". They
-- diverge exactly where this product lives — training vocabulary — and a
-- Brazilian reading European Portuguese gym copy notices immediately.
--
-- 'en' matches users.Locale's default in Go. Existing rows keep English, which
-- is what they have been reading.
ALTER TABLE users
  ADD COLUMN locale text NOT NULL DEFAULT 'en';

COMMENT ON COLUMN users.locale IS
  'BCP 47 tag chosen by the user: en, pt-PT, pt-BR or es. Not inferred from timezone.';

-- +goose Down
ALTER TABLE users
  DROP COLUMN locale;
