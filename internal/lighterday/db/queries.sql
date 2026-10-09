-- name: GetChoice :one
SELECT choice FROM lighter_days WHERE user_id = $1 AND day = $2;

-- name: SetChoice :exec
INSERT INTO lighter_days (user_id, day, choice) VALUES ($1, $2, $3)
ON CONFLICT (user_id, day) DO UPDATE SET choice = EXCLUDED.choice, decided_at = now();
