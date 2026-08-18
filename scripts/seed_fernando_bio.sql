-- seed_fernando_bio.sql
-- Apply against the LIVE North DB once Postgres is healthy.
-- Idempotent-ish: retires current biometrics, upserts prefs, ensures goal + memories.
--
-- Usage (example):
--   psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f scripts/seed_fernando_bio.sql
--
-- REQUIRED before apply: set :dob to Fernando's real date of birth (YYYY-MM-DD).
-- Age stated 2026-08-18: 38. Do NOT invent a birthday in production without him.

\if :{?email}
\else
\set email 'fernandocorreia316@gmail.com'
\endif

\if :{?dob}
\else
\echo 'ERROR: pass -v dob=YYYY-MM-DD (exact date of birth). Aborting.'
\quit 1
\endif

BEGIN;

-- Resolve user
CREATE TEMP TABLE _u AS
SELECT id
FROM users
WHERE lower(email) = lower(:'email')
LIMIT 1;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM _u) THEN
    RAISE EXCEPTION 'No users row for email % — create/login account first', current_setting('email', true);
  END IF;
END $$;

-- Biometrics: retire previous current, insert new current
UPDATE user_biometrics b
SET is_current = false
FROM _u u
WHERE b.user_id = u.id AND b.is_current;

INSERT INTO user_biometrics (user_id, weight_kg, height_cm, date_of_birth, sex, is_current)
SELECT u.id, 100.0, 185.0, :'dob'::date, 'male', true
FROM _u u;

-- Preferences: cutting + metric
INSERT INTO user_preferences (user_id, units_system, default_goal, default_macro_split, updated_at)
SELECT u.id, 'metric', 'cutting', 'moderate_carb', now()
FROM _u u
ON CONFLICT (user_id) DO UPDATE
SET units_system = EXCLUDED.units_system,
    default_goal = EXCLUDED.default_goal,
    default_macro_split = EXCLUDED.default_macro_split,
    updated_at = now();

-- Goal: hit 95kg (create if no active similar title)
INSERT INTO goals (user_id, title, motivation, success, category, status, target_date)
SELECT u.id,
       'Reach 95 kg body weight',
       'Cut from 100 kg while keeping strength and cardio cadence.',
       'Scale reads 95 kg on a normal morning weigh-in, held for 7 days.',
       'health',
       'active',
       NULL
FROM _u u
WHERE NOT EXISTS (
  SELECT 1 FROM goals g
  WHERE g.user_id = u.id
    AND g.status = 'active'
    AND g.title ILIKE '%95%kg%'
);

INSERT INTO goals (user_id, title, motivation, success, category, status)
SELECT u.id,
       'Hold training cadence: 4 strength + 3 cardio (7–10 km)',
       'Consistency over hero sessions.',
       'Most weeks: 4 strength sessions and 3 cardio sessions between 7 and 10 km.',
       'fitness',
       'active'
FROM _u u
WHERE NOT EXISTS (
  SELECT 1 FROM goals g
  WHERE g.user_id = u.id
    AND g.status = 'active'
    AND g.title ILIKE '%4 strength%'
);

-- Durable memories (approved, pinned)
INSERT INTO user_memories (user_id, category, content, status, pinned, source, confidence)
SELECT u.id, 'profile',
       'Body (2026-08-18): age 38, height 185 cm, weight 100 kg, goal weight 95 kg.',
       'approved', true, 'user', 1.0
FROM _u u
WHERE NOT EXISTS (
  SELECT 1 FROM user_memories m
  WHERE m.user_id = u.id AND m.deleted_at IS NULL
    AND m.content ILIKE '%185 cm%' AND m.content ILIKE '%100 kg%'
);

INSERT INTO user_memories (user_id, category, content, status, pinned, source, confidence)
SELECT u.id, 'training',
       'Training cadence: 4 strength workouts/week + 3 cardio sessions/week at 7–10 km each. Strength is Upper/Lower x2, machine-dominant, 2x5-10 @0-1 RIR.',
       'approved', true, 'user', 1.0
FROM _u u
WHERE NOT EXISTS (
  SELECT 1 FROM user_memories m
  WHERE m.user_id = u.id AND m.deleted_at IS NULL
    AND m.content ILIKE '%4 strength%' AND m.content ILIKE '%7–10 km%'
);

COMMIT;

\echo 'seed_fernando_bio applied for email=' :'email' ' dob=' :'dob'
