package hydration_test

import (
	"context"
	"testing"

	"github.com/NorthAIProject/north-client/internal/hydration"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

func newService(t *testing.T) (*hydration.Service, users.User) {
	t.Helper()

	pool := testdb.New(t)

	user, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        "fernando@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Fernando Correia",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	return hydration.NewService(hydration.NewRepository(pool)), user
}

// The point of an append log: several drinks in a day roll up into one total
// rather than overwriting each other.
func TestDrinksAccumulateAcrossTheDay(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	for _, amount := range []int{250, 500, 250} {
		if _, err := svc.Log(ctx, user, amount); err != nil {
			t.Fatalf("log %dml: %v", amount, err)
		}
	}

	day, err := svc.Today(ctx, user)
	if err != nil {
		t.Fatalf("today: %v", err)
	}

	if day.TotalML != 1000 {
		t.Errorf("TotalML = %d, want 1000", day.TotalML)
	}
	if day.Entries != 3 {
		t.Errorf("Entries = %d, want 3", day.Entries)
	}
	if day.Percent() != 50 { // 1000 of the 2000ml default target
		t.Errorf("Percent() = %d, want 50", day.Percent())
	}
}

func TestAnEmptyDayReportsZeroRatherThanFailing(t *testing.T) {
	svc, user := newService(t)

	day, err := svc.Today(context.Background(), user)
	if err != nil {
		t.Fatalf("today: %v", err)
	}
	if day.TotalML != 0 || day.Entries != 0 {
		t.Errorf("empty day = %+v, want zeroes", day)
	}
	if day.Summary() != "Water today: nothing logged" {
		t.Errorf("Summary() = %q", day.Summary())
	}
}

func TestUndoRemovesADrinkFromTheTotal(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	entry, err := svc.Log(ctx, user, 500)
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if _, err := svc.Log(ctx, user, 250); err != nil {
		t.Fatalf("log: %v", err)
	}

	if err := svc.Undo(ctx, user, entry.ID); err != nil {
		t.Fatalf("undo: %v", err)
	}

	day, err := svc.Today(ctx, user)
	if err != nil {
		t.Fatalf("today: %v", err)
	}
	if day.TotalML != 250 {
		t.Errorf("TotalML after undo = %d, want 250", day.TotalML)
	}
}

func TestDrinksAreScopedToTheirOwner(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	if _, err := svc.Log(ctx, user, 500); err != nil {
		t.Fatalf("log: %v", err)
	}

	// Undo with a real id but the wrong owner must not delete anything. The
	// query filters on user_id, so this is a silent no-op rather than an
	// error — assert on the total, which is what would actually be wrong.
	entries, err := svc.TodayEntries(ctx, user)
	if err != nil {
		t.Fatalf("entries: %v", err)
	}

	stranger := user
	stranger.ID = users.User{}.ID // zero uuid, definitely not the owner
	if err := svc.Undo(ctx, stranger, entries[0].ID); err != nil {
		t.Fatalf("undo by stranger: %v", err)
	}

	day, err := svc.Today(ctx, user)
	if err != nil {
		t.Fatalf("today: %v", err)
	}
	if day.TotalML != 500 {
		t.Errorf("a stranger deleted someone's entry: total = %d, want 500", day.TotalML)
	}
}

func TestImplausibleAmountsAreRejected(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	for _, amount := range []int{0, -250, 6000} {
		if _, err := svc.Log(ctx, user, amount); !apperr.Is(err, apperr.ErrValidation) {
			t.Errorf("Log(%d) error = %v, want validation error", amount, err)
		}
	}
}
