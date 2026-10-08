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
	perMealWeekdays = 20261004120000 // days as a weekday copied onto each meal
	mealPlanDays    = 20261008160000 // days as rows of their own
)

// Production ran 20261004120000 before 20261008160000 replaced its shape, so
// the replacement has to carry real rows across: meals with and without a
// weekday, two meals sharing a number on different weekdays, a plan with no
// meals, and a plan with no type.
func TestMealPlanDaysMigrationKeepsExistingPlans(t *testing.T) {
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

	const dbName = "north_meal_days_check"
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

	if _, err = provider.UpTo(ctx, perMealWeekdays); err != nil {
		t.Fatalf("migrate to %d: %v", perMealWeekdays, err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, execErr := db.ExecContext(ctx, query, args...); execErr != nil {
			t.Fatalf("%s: %v", query, execErr)
		}
	}
	exec(`INSERT INTO users (id, email, password_hash, display_name, timezone)
		VALUES ('00000000-0000-0000-0000-000000000001', 'm@north.test', 'x', 'M', 'UTC')`)
	exec(`INSERT INTO meal_plans (id, user_id, name, plan_type, custom_carb_pct) VALUES
		('00000000-0000-0000-0000-0000000000a1', '00000000-0000-0000-0000-000000000001', 'Typed', 'low_carb', 40),
		('00000000-0000-0000-0000-0000000000a2', '00000000-0000-0000-0000-000000000001', 'Untyped', NULL, NULL),
		('00000000-0000-0000-0000-0000000000a3', '00000000-0000-0000-0000-000000000001', 'Empty', 'custom', NULL)`)
	exec(`INSERT INTO meals (meal_plan_id, meal_number, name, weekday, day_plan_type) VALUES
		('00000000-0000-0000-0000-0000000000a1', 1, 'Saturday breakfast', 6, 'high_carb'),
		('00000000-0000-0000-0000-0000000000a1', 1, 'Any-day snack', NULL, NULL),
		('00000000-0000-0000-0000-0000000000a1', 1, 'Monday breakfast', 1, NULL),
		('00000000-0000-0000-0000-0000000000a2', 1, 'Lunch', NULL, NULL)`)

	if _, err = provider.UpTo(ctx, mealPlanDays); err != nil {
		t.Fatalf("migrate to %d: %v", mealPlanDays, err)
	}

	// Plans all have a type and start advanced; a custom plan without its
	// share falls back to mid carb, and a preset keeps no stray share.
	rows, err := db.QueryContext(ctx, `SELECT name, plan_type, custom_carb_pct IS NULL, mode FROM meal_plans ORDER BY name`)
	if err != nil {
		t.Fatalf("read plans: %v", err)
	}
	want := map[string]string{"Empty": "mid_carb", "Typed": "low_carb", "Untyped": "mid_carb"}
	for rows.Next() {
		var name, planType, mode string
		var noPct bool
		if err = rows.Scan(&name, &planType, &noPct, &mode); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		if planType != want[name] || !noPct || mode != "advanced" {
			t.Errorf("%s: type %s, no share %v, mode %s", name, planType, noPct, mode)
		}
	}
	_ = rows.Close()

	// Typed gets Monday (its own meal plus the any-day one, renumbered) and
	// Saturday; Untyped and Empty get Monday.
	var days, mondayMeals, mondayNumbers int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM meal_plan_days`).Scan(&days); err != nil {
		t.Fatalf("count days: %v", err)
	}
	if err = db.QueryRowContext(ctx, `
		SELECT count(*), count(DISTINCT m.meal_number) FROM meals m
		JOIN meal_plan_days d ON d.id = m.day_id
		WHERE d.meal_plan_id = '00000000-0000-0000-0000-0000000000a1' AND d.weekday = 1`,
	).Scan(&mondayMeals, &mondayNumbers); err != nil {
		t.Fatalf("count monday meals: %v", err)
	}
	if days != 4 || mondayMeals != 2 || mondayNumbers != 2 {
		t.Errorf("days = %d, Typed's Monday has %d meals with %d numbers; want 4, 2, 2", days, mondayMeals, mondayNumbers)
	}

	// And back: each meal regains its day's weekday.
	if _, err = provider.DownTo(ctx, perMealWeekdays); err != nil {
		t.Fatalf("migrate down to %d: %v", perMealWeekdays, err)
	}
	var saturday int
	if err = db.QueryRowContext(ctx, `SELECT weekday FROM meals WHERE name = 'Saturday breakfast'`).Scan(&saturday); err != nil {
		t.Fatalf("read weekday after down: %v", err)
	}
	if saturday != 6 {
		t.Errorf("Saturday breakfast is on weekday %d after down", saturday)
	}
}
