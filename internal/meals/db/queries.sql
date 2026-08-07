-- Ingredients

-- name: CreateIngredient :one
INSERT INTO ingredients (
    user_id, name, brand, category, serving_size_grams,
    calories_per_100g, protein_g_per_100g, fat_g_per_100g, saturated_fat_g_per_100g, carbs_g_per_100g,
    fiber_g_per_100g, sugar_g_per_100g, sodium_mg_per_100g, potassium_mg_per_100g, cholesterol_mg_per_100g
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
RETURNING *;

-- name: GetIngredient :one
SELECT * FROM ingredients WHERE id = $1;

-- name: SearchIngredients :many
-- Visible ingredients are the shared/global set plus the user's own.
SELECT * FROM ingredients
WHERE (user_id IS NULL OR user_id = $1) AND lower(name) LIKE lower($2)
ORDER BY name
LIMIT $3;

-- name: UpdateIngredient :one
-- Only a user's own ingredients can be edited; shared ones are read-only to
-- everyone, which this WHERE clause enforces by requiring ownership.
UPDATE ingredients
SET name = $3, brand = $4, category = $5, serving_size_grams = $6,
    calories_per_100g = $7, protein_g_per_100g = $8, fat_g_per_100g = $9, saturated_fat_g_per_100g = $10,
    carbs_g_per_100g = $11, fiber_g_per_100g = $12, sugar_g_per_100g = $13, sodium_mg_per_100g = $14,
    potassium_mg_per_100g = $15, cholesterol_mg_per_100g = $16, updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: DeleteIngredient :exec
DELETE FROM ingredients WHERE id = $1 AND user_id = $2;

-- Diets

-- name: ListDiets :many
SELECT * FROM diets ORDER BY name;

-- name: UserDiets :many
SELECT d.* FROM diets d
JOIN user_diet_preferences udp ON udp.diet_id = d.id
WHERE udp.user_id = $1
ORDER BY d.name;

-- name: DeleteUserDiets :exec
DELETE FROM user_diet_preferences WHERE user_id = $1;

-- name: AddUserDiet :exec
INSERT INTO user_diet_preferences (user_id, diet_id)
VALUES ($1, $2)
ON CONFLICT (user_id, diet_id) DO NOTHING;

-- name: RemoveUserDiet :exec
DELETE FROM user_diet_preferences WHERE user_id = $1 AND diet_id = $2;

-- Meal plans

-- name: CreateMealPlan :one
INSERT INTO meal_plans (user_id, name, description, objective, activity_level, gender)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetMealPlan :one
SELECT * FROM meal_plans WHERE id = $1 AND user_id = $2;

-- name: ListMealPlans :many
SELECT * FROM meal_plans WHERE user_id = $1 ORDER BY created_at DESC;

-- name: UpdateMealPlan :one
UPDATE meal_plans
SET name = $3, description = $4, objective = $5, activity_level = $6, gender = $7, updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: DeleteMealPlan :exec
DELETE FROM meal_plans WHERE id = $1 AND user_id = $2;

-- name: UpdateMealPlanTotalMacros :exec
UPDATE meal_plans SET total_macros = $2, updated_at = now() WHERE id = $1;

-- Meals (within a plan)

-- name: CreateMeal :one
INSERT INTO meals (meal_plan_id, meal_number, name)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetMealOwned :one
-- Ownership is via the parent plan, so a stranger holding the exact meal id
-- still finds nothing.
SELECT m.* FROM meals m
JOIN meal_plans mp ON mp.id = m.meal_plan_id
WHERE m.id = $1 AND mp.user_id = $2;

-- name: ListMealsByPlan :many
SELECT * FROM meals WHERE meal_plan_id = $1 ORDER BY meal_number;

-- name: DeleteMealOwned :exec
DELETE FROM meals
USING meal_plans
WHERE meals.id = $1 AND meals.meal_plan_id = meal_plans.id AND meal_plans.user_id = $2;

-- name: UpdateMealTotalMacros :exec
UPDATE meals SET total_macros = $2 WHERE id = $1;

-- Meal ingredients

-- name: CreateMealIngredient :one
INSERT INTO meal_ingredients (meal_id, ingredient_id, quantity_grams, calories, protein_g, fat_g, carbs_g)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: ListMealIngredients :many
-- ingredient_name is denormalized for display without a second round trip.
SELECT mi.*, i.name AS ingredient_name
FROM meal_ingredients mi
JOIN ingredients i ON i.id = mi.ingredient_id
WHERE mi.meal_id = $1
ORDER BY mi.created_at;

-- name: GetMealIngredientOwned :one
-- Ownership is via meal -> plan, two joins deep.
SELECT mi.*, mi.meal_id AS owned_meal_id
FROM meal_ingredients mi
JOIN meals m ON m.id = mi.meal_id
JOIN meal_plans mp ON mp.id = m.meal_plan_id
WHERE mi.id = $1 AND mp.user_id = $2;

-- name: DeleteMealIngredient :exec
DELETE FROM meal_ingredients WHERE id = $1;

-- name: SumMealIngredientMacros :one
SELECT
    COALESCE(SUM(calories), 0)::double precision  AS calories,
    COALESCE(SUM(protein_g), 0)::double precision AS protein_g,
    COALESCE(SUM(fat_g), 0)::double precision     AS fat_g,
    COALESCE(SUM(carbs_g), 0)::double precision   AS carbs_g
FROM meal_ingredients
WHERE meal_id = $1;

-- Food logs

-- name: InsertFoodLog :one
INSERT INTO food_logs (user_id, log_date, meal_id, ingredient_id, quantity_grams, label, calories, protein_g, fat_g, carbs_g)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: DeleteFoodLog :exec
DELETE FROM food_logs WHERE id = $1 AND user_id = $2;

-- name: ListFoodLogsByDate :many
SELECT * FROM food_logs WHERE user_id = $1 AND log_date = $2 ORDER BY logged_at;

-- name: ListFoodLogsByRange :many
SELECT * FROM food_logs WHERE user_id = $1 AND log_date BETWEEN $2 AND $3 ORDER BY log_date, logged_at;

-- name: DailyFoodLogTotals :one
SELECT
    COALESCE(SUM(calories), 0)::double precision  AS calories,
    COALESCE(SUM(protein_g), 0)::double precision AS protein_g,
    COALESCE(SUM(fat_g), 0)::double precision     AS fat_g,
    COALESCE(SUM(carbs_g), 0)::double precision   AS carbs_g
FROM food_logs
WHERE user_id = $1 AND log_date = $2;

-- Meal reminders

-- name: CreateMealReminder :one
INSERT INTO meal_reminders (user_id, label, time_of_day, days_of_week)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetMealReminder :one
SELECT * FROM meal_reminders WHERE id = $1 AND user_id = $2;

-- name: ListMealReminders :many
SELECT * FROM meal_reminders WHERE user_id = $1 ORDER BY time_of_day;

-- name: UpdateMealReminder :one
UPDATE meal_reminders
SET label = $3, time_of_day = $4, days_of_week = $5, updated_at = now()
WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: DeleteMealReminder :exec
DELETE FROM meal_reminders WHERE id = $1 AND user_id = $2;

-- name: SetMealReminderEnabled :one
UPDATE meal_reminders SET enabled = $3, updated_at = now() WHERE id = $1 AND user_id = $2
RETURNING *;

-- name: ListNotYetFiredMealReminders :many
-- Candidates for today: enabled, and not already marked fired for this local
-- date. Day-of-week and time-of-day matching happens in Go via Reminder.DueOn,
-- which keeps that logic in one place instead of splitting it across SQL and
-- Go.
SELECT * FROM meal_reminders
WHERE user_id = $1 AND enabled AND (last_fired_local_date IS DISTINCT FROM sqlc.arg(as_of_date)::date);

-- name: MarkMealReminderFired :exec
UPDATE meal_reminders SET last_fired_local_date = $2, updated_at = now() WHERE id = $1;
