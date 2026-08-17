package account_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/account"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/documents"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/memories"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

func seedUser(t *testing.T, pool *pgxpool.Pool, email string) users.User {
	t.Helper()

	u, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        email,
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Sam",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatalf("register %s: %v", email, err)
	}
	return u
}

// fakeStorage records what was asked of the bucket, and can be told to refuse.
type fakeStorage struct {
	deleted []string
	fail    bool
}

func (f *fakeStorage) Delete(_ context.Context, key string) error {
	f.deleted = append(f.deleted, key)
	if f.fail {
		return errors.New("bucket unavailable")
	}
	return nil
}

// fill gives an account something to lose across as many slices as the delete
// has to reach.
func fill(t *testing.T, pool *pgxpool.Pool, user users.User) {
	t.Helper()
	ctx := context.Background()

	goalSvc := goals.NewService(goals.NewRepository(pool))
	if _, err := goalSvc.Create(ctx, user.ID, goals.Input{
		Title: "Press bodyweight overhead", Category: goals.CategoryFitness,
		Motivation: "Because I said I would.", Success: "One clean rep.",
	}); err != nil {
		t.Fatal(err)
	}

	checkinSvc := checkins.NewService(checkins.NewRepository(pool), goalSvc)
	if _, err := checkinSvc.UpsertToday(ctx, user, checkins.Input{
		Mood: 4, Energy: 3, Wins: "Trained", Challenges: "Slept badly",
	}); err != nil {
		t.Fatal(err)
	}

	memorySvc := memories.NewService(memories.NewRepository(pool))
	if _, err := memorySvc.Create(ctx, user.ID, memories.Input{
		Category: memories.CategoryInjury, Content: "Left shoulder dislocates",
	}); err != nil {
		t.Fatal(err)
	}

	convoSvc := conversations.NewService(conversations.NewRepository(pool))
	convo, err := convoSvc.Start(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := convoSvc.AppendUserMessage(ctx, convo.ID, "Can I press overhead?", nil); err != nil {
		t.Fatal(err)
	}

	docSvc := documents.NewService(documents.NewRepository(pool), nil, nil)
	if _, err := docSvc.CreateNote(ctx, user.ID, "Physio notes", "Narrow grip only."); err != nil {
		t.Fatal(err)
	}
}

func countFor(t *testing.T, pool *pgxpool.Pool, table string, userID uuid.UUID) int {
	t.Helper()

	var n int
	// Table names are literals from the test itself, never input.
	if err := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM "+table+" WHERE user_id = $1", userID).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func newService(pool *pgxpool.Pool, storage account.Storage) *account.Service {
	return account.NewService(account.NewRepository(pool), storage, nil)
}

// The claim is that leaving actually removes you. Anything less is a setting
// that says "deleted" next to data that is still there.
func TestDeletingAnAccountLeavesNothingBehind(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "leaving@north.test")
	fill(t, pool, user)

	tables := []string{"goals", "check_ins", "user_memories", "conversations", "documents"}
	for _, table := range tables {
		if countFor(t, pool, table, user.ID) == 0 {
			t.Fatalf("%s was empty before the delete, so the test proves nothing", table)
		}
	}

	if _, err := newService(pool, &fakeStorage{}).Delete(ctx, user, user.Email); err != nil {
		t.Fatalf("delete: %v", err)
	}

	for _, table := range tables {
		if n := countFor(t, pool, table, user.ID); n != 0 {
			t.Errorf("%s still holds %d rows for the deleted account", table, n)
		}
	}

	var users int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM users WHERE id = $1", user.ID).Scan(&users); err != nil {
		t.Fatal(err)
	}
	if users != 0 {
		t.Error("the account row survived its own deletion")
	}
}

// Signing out everywhere is the half of deletion a person can actually observe,
// and the ticket's "cannot access deleted account resources" rests on it.
func TestSessionsDieWithTheAccount(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "sessions@north.test")

	sessions := auth.NewSessionStore(pool, 24*time.Hour)
	token, _, err := sessions.Create(ctx, user.ID, auth.Metadata{})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := sessions.Resolve(ctx, token); err != nil {
		t.Fatalf("the session was not live before the delete: %v", err)
	}

	if _, err := newService(pool, &fakeStorage{}).Delete(ctx, user, user.Email); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, err := sessions.Resolve(ctx, token); !apperr.Is(err, apperr.ErrUnauthenticated) {
		t.Errorf("a session outlived the account it belonged to: err = %v", err)
	}
}

// A meal logged with an ingredient the person entered themselves used to make
// the account undeletable: ingredients cascade with the user, and
// meal_ingredients pointed at them with RESTRICT. Without the FK migration this
// test fails on the delete, which is the whole reason it exists.
func TestAMealWithASelfCreatedIngredientDoesNotBlockDeletion(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "cook@north.test")

	var ingredientID, planID, mealID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO ingredients (user_id, name, calories_per_100g)
		VALUES ($1, 'Grandma''s stew', 120) RETURNING id`, user.ID).Scan(&ingredientID); err != nil {
		t.Fatalf("seed ingredient: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO meal_plans (user_id, name) VALUES ($1, 'Winter') RETURNING id`,
		user.ID).Scan(&planID); err != nil {
		t.Fatalf("seed meal plan: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO meals (meal_plan_id, meal_number, name)
		VALUES ($1, 1, 'Dinner') RETURNING id`, planID).Scan(&mealID); err != nil {
		t.Fatalf("seed meal: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO meal_ingredients (meal_id, ingredient_id, quantity_grams, calories, protein_g, fat_g, carbs_g)
		VALUES ($1, $2, 300, 360, 20, 12, 30)`, mealID, ingredientID); err != nil {
		t.Fatalf("seed meal ingredient: %v", err)
	}

	if _, err := newService(pool, &fakeStorage{}).Delete(ctx, user, user.Email); err != nil {
		t.Fatalf("a self-created ingredient blocked the deletion: %v", err)
	}

	if n := countFor(t, pool, "meal_plans", user.ID); n != 0 {
		t.Errorf("meal_plans still holds %d rows", n)
	}
}

// Relaxing that foreign key was in aid of one thing only. The rule 00015 wrote
// it for — an ingredient in use cannot simply vanish out from under a meal —
// still has to hold on its own, or the migration bought deletability by giving
// up something that mattered.
func TestDeletingAnIngredientStillInUseStillFails(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "chef@north.test")

	var ingredientID, planID, mealID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO ingredients (user_id, name, calories_per_100g)
		VALUES ($1, 'House sauce', 90) RETURNING id`, user.ID).Scan(&ingredientID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO meal_plans (user_id, name) VALUES ($1, 'Winter') RETURNING id`,
		user.ID).Scan(&planID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO meals (meal_plan_id, meal_number, name)
		VALUES ($1, 1, 'Dinner') RETURNING id`, planID).Scan(&mealID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO meal_ingredients (meal_id, ingredient_id, quantity_grams, calories, protein_g, fat_g, carbs_g)
		VALUES ($1, $2, 100, 90, 2, 1, 18)`, mealID, ingredientID); err != nil {
		t.Fatal(err)
	}

	// Deferred now, so the refusal arrives at commit rather than at the delete.
	// It still arrives.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "DELETE FROM ingredients WHERE id = $1", ingredientID); err != nil {
		return // Refused immediately, which is also fine.
	}
	if err := tx.Commit(ctx); err == nil {
		t.Error("an ingredient still used by a meal was deleted out from under it")
	}
}

func TestAWrongConfirmationDeletesNothing(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "careful@north.test")
	fill(t, pool, user)

	_, err := newService(pool, &fakeStorage{}).Delete(ctx, user, "someone-else@north.test")

	var fieldErrs apperr.FieldErrors
	if !apperr.As(err, &fieldErrs) {
		t.Fatalf("a mismatched confirmation should be a field error, got %v", err)
	}
	if _, ok := fieldErrs.Messages()[account.ConfirmField]; !ok {
		t.Errorf("the error is not attached to %q: %v", account.ConfirmField, fieldErrs.Messages())
	}
	if n := countFor(t, pool, "goals", user.ID); n == 0 {
		t.Error("a refused deletion removed data anyway")
	}
}

// The address is typed by hand, so the casing will not match the stored one
// half the time. The column is citext; the confirmation should agree with it.
func TestConfirmationIgnoresCaseAndSurroundingSpace(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool, "shouty@north.test")

	if _, err := newService(pool, &fakeStorage{}).Delete(
		context.Background(), user, "  SHOUTY@North.Test  "); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestDeletingOneAccountLeavesAnotherAlone(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	leaving := seedUser(t, pool, "leaver@north.test")
	staying := seedUser(t, pool, "stayer@north.test")
	fill(t, pool, leaving)
	fill(t, pool, staying)

	if _, err := newService(pool, &fakeStorage{}).Delete(ctx, leaving, leaving.Email); err != nil {
		t.Fatalf("delete: %v", err)
	}

	for _, table := range []string{"goals", "check_ins", "user_memories", "conversations", "documents"} {
		if n := countFor(t, pool, table, staying.ID); n == 0 {
			t.Errorf("deleting one account emptied %s for another", table)
		}
	}
}

// The bucket does not cascade, so the keys have to be read off the rows before
// those rows are gone. Getting the order wrong orphans the bytes silently.
func TestStoredFilesAreDeletedWithTheAccount(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "files@north.test")

	var liveDoc, goneDoc uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO documents (user_id, title, source_kind, storage_key, mime, byte_size)
		VALUES ($1, 'Scan', 'upload', 'users/x/documents/live.pdf', 'application/pdf', 10)
		RETURNING id`, user.ID).Scan(&liveDoc); err != nil {
		t.Fatalf("seed document: %v", err)
	}
	// A soft-deleted document is hidden from the person but its bytes are still
	// in the bucket, and an account being erased wants them gone either way.
	if err := pool.QueryRow(ctx, `
		INSERT INTO documents (user_id, title, source_kind, storage_key, mime, byte_size, deleted_at)
		VALUES ($1, 'Old scan', 'upload', 'users/x/documents/gone.pdf', 'application/pdf', 10, now())
		RETURNING id`, user.ID).Scan(&goneDoc); err != nil {
		t.Fatalf("seed soft-deleted document: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO media (user_id, kind, mime_type, size_bytes, storage_key, original_name)
		VALUES ($1, 'video', 'video/mp4', 20, 'users/x/2026/08/clip.mp4', 'clip.mp4')`,
		user.ID); err != nil {
		t.Fatalf("seed media: %v", err)
	}

	storage := &fakeStorage{}
	result, err := newService(pool, storage).Delete(ctx, user, user.Email)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}

	want := map[string]bool{
		"users/x/documents/live.pdf": true,
		"users/x/documents/gone.pdf": true,
		"users/x/2026/08/clip.mp4":   true,
	}
	if len(storage.deleted) != len(want) {
		t.Fatalf("asked the bucket for %v, want %d keys", storage.deleted, len(want))
	}
	for _, key := range storage.deleted {
		if !want[key] {
			t.Errorf("deleted an unexpected object %q", key)
		}
	}
	if result.StorageObjects != len(want) || result.StorageFailures != 0 {
		t.Errorf("erasure reported %d objects and %d failures, want %d and 0",
			result.StorageObjects, result.StorageFailures, len(want))
	}
}

// A bucket that will not let go of a file must not turn a completed deletion
// into a failure the person is asked to retry. The account is already gone by
// then; all that is left to do is say so and count it.
func TestAStorageFailureDoesNotUndoTheDeletion(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "stuck@north.test")

	if _, err := pool.Exec(ctx, `
		INSERT INTO documents (user_id, title, source_kind, storage_key, mime, byte_size)
		VALUES ($1, 'Scan', 'upload', 'users/x/documents/stuck.pdf', 'application/pdf', 10)`,
		user.ID); err != nil {
		t.Fatalf("seed document: %v", err)
	}

	result, err := newService(pool, &fakeStorage{fail: true}).Delete(ctx, user, user.Email)
	if err != nil {
		t.Fatalf("a stuck object failed the whole deletion: %v", err)
	}
	if result.StorageFailures != 1 {
		t.Errorf("erasure reported %d storage failures, want 1", result.StorageFailures)
	}

	var n int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM users WHERE id = $1", user.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Error("the account survived a storage failure")
	}
}

// The record of a deletion cannot live in a table that cascades with the user,
// or it deletes itself in the same statement that writes it.
func TestTheRecordOfTheDeletionOutlivesTheAccount(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "recorded@north.test")

	if _, err := newService(pool, &fakeStorage{}).Delete(ctx, user, user.Email); err != nil {
		t.Fatalf("delete: %v", err)
	}

	var event string
	var detail []byte
	if err := pool.QueryRow(ctx,
		"SELECT event, detail FROM account_events WHERE user_id = $1", user.ID).Scan(&event, &detail); err != nil {
		t.Fatalf("no record of the deletion survived: %v", err)
	}
	if event != account.EventDelete {
		t.Errorf("recorded event = %q, want %q", event, account.EventDelete)
	}
	if len(detail) == 0 {
		t.Error("the deletion record carries no detail")
	}
}

func TestAnExportIsRecorded(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "exporter@north.test")

	if err := newService(pool, &fakeStorage{}).RecordExport(ctx, user.ID); err != nil {
		t.Fatalf("record export: %v", err)
	}

	var event string
	if err := pool.QueryRow(ctx,
		"SELECT event FROM account_events WHERE user_id = $1", user.ID).Scan(&event); err != nil {
		t.Fatalf("the export was not recorded: %v", err)
	}
	if event != account.EventExport {
		t.Errorf("recorded event = %q, want %q", event, account.EventExport)
	}
}
