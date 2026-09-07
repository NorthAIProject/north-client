-- +goose Up
-- Repairs notification links that point at a route which has never existed.
--
-- The daily training nudge was built with "/app/workouts/" + plan id, while the
-- routes have always been mounted under /training (internal/workouts/handler.go).
-- user_nudges.href is persisted, so every workout_today nudge ever raised holds
-- a URL that 404s from the bell, from the "read" redirect, and from a push tap.
--
-- The emitter is fixed as of this migration, and a permanent redirect covers the
-- notifications already delivered to devices, which nothing here can reach. This
-- repairs the rows still sitting in the table.
UPDATE user_nudges
   SET href = '/app/training/' || substring(href from '^/app/workouts/(.*)$')
 WHERE href LIKE '/app/workouts/%';

-- The bare form, with no plan id, was only ever written by a test fixture.
UPDATE user_nudges
   SET href = '/app/training'
 WHERE href = '/app/workouts';

-- +goose Down
-- Deliberately not reversed. The old value was a 404 on every surface that
-- rendered it; restoring it would only re-break links that now work.
SELECT 1;
