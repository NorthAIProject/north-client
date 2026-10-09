package database_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/NorthAIProject/north-client/migrations"
)

const (
	beforeMealOptions = 20261009130000 // the migration just before options
	mealOptions       = 20261009150000 // a meal slot holds several options
)

// Existing meals become their slot's first option; a slot then takes further
// options under the same meal number; and Down drops those alternatives so
// the one-meal-per-number constraint can come back.
func TestMealOptionsMigrationKeepsExistingMeals(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = os.Getenv("DATABASE_URL")
	}
	if url == "" {
		t.Skip("no TEST_DATABASE_URL or DATABASE_URL set; skipping database test")
	}
	ctx := context.Background()

	admin, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open admin connection: %v", err)
	}
	defer func() { _ = admin.Close() }()

	const dbName = "north_meal_options_check"
	if _, err = admin.ExecContext(ctx, `DROP DATABASE IF EXISTS `+dbName+` WITH (FORCE)`); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if _, err = admin.ExecContext(ctx, `CREATE DATABASE `+dbName); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(context.Background(), `DROP DATABASE IF EXISTS `+dbName+` WITH (FORCE)`)
	})

	db, err := sql.Open("pgx", swapDatabase(url, dbName))
	if err != nil {
		t.Fatalf("open target: %v", err)
	}
	defer func() { _ = db.Close() }()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS, goose.WithAllowOutofOrder(true))
	if err != nil {
		t.Fatalf("migration provider: %v", err)
	}

	if _, err = provider.UpTo(ctx, beforeMealOptions); err != nil {
		t.Fatalf("migrate to %d: %v", beforeMealOptions, err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, execErr := db.ExecContext(ctx, query, args...); execErr != nil {
			t.Fatalf("%s: %v", query, execErr)
		}
	}
	const (
		plan = "00000000-0000-0000-0000-0000000000a1"
		day  = "00000000-0000-0000-0000-0000000000d1"
	)
	exec(`INSERT INTO users (id, email, password_hash, display_name, timezone)
		VALUES ('00000000-0000-0000-0000-000000000001', 'o@north.test', 'x', 'O', 'UTC')`)
	exec(`INSERT INTO meal_plans (id, user_id, name, plan_type, mode)
		VALUES ($1, '00000000-0000-0000-0000-000000000001', 'Plan', 'mid_carb', 'easy')`, plan)
	exec(`INSERT INTO meal_plan_days (id, meal_plan_id, weekday) VALUES ($1, $2, 1)`, day, plan)
	exec(`INSERT INTO meals (meal_plan_id, day_id, meal_number, name) VALUES ($1, $2, 1, 'Breakfast')`, plan, day)

	if _, err = provider.UpTo(ctx, mealOptions); err != nil {
		t.Fatalf("migrate to %d: %v", mealOptions, err)
	}

	var optionIndex int
	var label string
	if err = db.QueryRowContext(ctx, `SELECT option_index, option_label FROM meals WHERE name = 'Breakfast'`).Scan(&optionIndex, &label); err != nil {
		t.Fatalf("read existing meal: %v", err)
	}
	if optionIndex != 1 || label != "" {
		t.Errorf("existing meal is option %d labelled %q; want 1 and no label", optionIndex, label)
	}

	// A second option of the same slot is allowed; a second option 2 is not.
	exec(`INSERT INTO meals (meal_plan_id, day_id, meal_number, name, option_index, option_label)
		VALUES ($1, $2, 1, 'Breakfast', 2, 'Opção 2')`, plan, day)
	if _, err = db.ExecContext(ctx, `INSERT INTO meals (meal_plan_id, day_id, meal_number, name, option_index)
		VALUES ($1, $2, 1, 'Breakfast', 2)`, plan, day); err == nil {
		t.Error("a slot took two options numbered 2")
	}

	// And back: the alternative goes, the default stays, and one meal per
	// number is enforced again.
	if _, err = provider.DownTo(ctx, beforeMealOptions); err != nil {
		t.Fatalf("migrate down to %d: %v", beforeMealOptions, err)
	}
	var meals int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM meals`).Scan(&meals); err != nil {
		t.Fatalf("count meals after down: %v", err)
	}
	if meals != 1 {
		t.Errorf("%d meals after down; want only the default", meals)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO meals (meal_plan_id, day_id, meal_number, name)
		VALUES ($1, $2, 1, 'Second breakfast')`, plan, day); err == nil {
		t.Error("after down, a day took two meals numbered 1")
	}
}
