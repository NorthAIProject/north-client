-- name: InsertPost :one
-- The once-a-day claim: a second post for the same day inserts nothing, so
-- two sweeps racing cannot both publish.
INSERT INTO daily_posts (post_date, theme, title, body, ai_generated, model, prompt_version)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (post_date) DO NOTHING
RETURNING id;

-- name: PostExists :one
SELECT EXISTS (SELECT 1 FROM daily_posts WHERE post_date = $1);

-- name: RecentPosts :many
-- Newest first, on or before a day, with the viewer's kudos.
SELECT p.id, p.post_date, p.theme, p.title, p.body, p.ai_generated, p.created_at,
       (SELECT count(*) FROM daily_post_kudos k WHERE k.post_id = p.id) AS kudos,
       EXISTS (SELECT 1 FROM daily_post_kudos k WHERE k.post_id = p.id AND k.user_id = sqlc.arg('viewer')) AS kudoed
FROM daily_posts p
WHERE p.post_date <= sqlc.arg('on_or_before')
ORDER BY p.post_date DESC
LIMIT sqlc.arg('lim');

-- name: RecentPostThemes :many
-- What the last few posts were about, so the next one is about something else.
SELECT theme, title FROM daily_posts
WHERE post_date < sqlc.arg('before')
ORDER BY post_date DESC
LIMIT sqlc.arg('lim');

-- name: PostIDExists :one
SELECT EXISTS (SELECT 1 FROM daily_posts WHERE id = $1);

-- name: GiveKudos :exec
INSERT INTO daily_post_kudos (post_id, user_id) VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: TakeKudos :exec
DELETE FROM daily_post_kudos WHERE post_id = $1 AND user_id = $2;

-- name: WeekCheckIns :one
-- Everyone's check-ins over a window, as counts and averages only: the post
-- may lean on how people are doing, never on who.
SELECT count(DISTINCT user_id)::int AS people,
       coalesce(avg(energy), 0)::float8 AS avg_energy,
       coalesce(avg(mood), 0)::float8 AS avg_mood
FROM check_ins
WHERE local_date >= sqlc.arg('since') AND local_date < sqlc.arg('until');

-- name: WeekSleep :one
SELECT count(DISTINCT user_id)::int AS people,
       coalesce(avg(duration_minutes), 0)::float8 AS avg_minutes
FROM sleep_logs
WHERE local_date >= sqlc.arg('since') AND local_date < sqlc.arg('until');
