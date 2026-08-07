-- name: GetUserPreferences :one
SELECT * FROM user_preferences WHERE user_id = $1;

-- name: UpsertUserPreferences :one
INSERT INTO user_preferences (user_id, units_system, default_goal, default_macro_split)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id) DO UPDATE
SET units_system = $2, default_goal = $3, default_macro_split = $4, updated_at = now()
RETURNING *;
