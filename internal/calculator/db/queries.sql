-- name: UnsetCurrentMacroPlans :exec
UPDATE user_macro_plans SET is_current = false WHERE user_id = $1 AND is_current;

-- name: InsertMacroPlan :one
INSERT INTO user_macro_plans (
    user_id, weight_kg, height_cm, age, sex,
    activity_level, goal, macro_split,
    bmr, tdee, calorie_goal, protein_g, fat_g, carb_g,
    is_current
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, true)
RETURNING *;

-- name: CurrentMacroPlan :one
SELECT * FROM user_macro_plans WHERE user_id = $1 AND is_current;

-- name: ListMacroPlans :many
SELECT * FROM user_macro_plans
WHERE user_id = $1
ORDER BY created_at DESC
LIMIT $2;
