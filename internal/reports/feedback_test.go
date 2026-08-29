package reports_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func TestRatingAReportPersistsAndCanBeCleared(t *testing.T) {
	svc, owner, _, _, _ := fixture(t)
	ctx := context.Background()

	item, err := svc.RequestGenerate(ctx, owner.ID, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if item.Rated() {
		t.Fatal("a fresh report is already rated")
	}

	yes := true
	rated, err := svc.SetHelpful(ctx, item.ID, owner.ID, &yes)
	if err != nil {
		t.Fatal(err)
	}
	if !rated.RatedHelpful() {
		t.Errorf("Helpful = %v, want true", rated.Helpful)
	}

	// Reloaded, so this proves a write rather than a returned value.
	reloaded, err := svc.Get(ctx, item.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.RatedHelpful() {
		t.Errorf("reloaded Helpful = %v, want true", reloaded.Helpful)
	}

	cleared, err := svc.SetHelpful(ctx, item.ID, owner.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.Rated() {
		t.Errorf("after clearing, Helpful = %v, want nil", cleared.Helpful)
	}
}

// Scoped in the UPDATE statement, like archiving is, so a report id from another
// account updates nothing rather than relying on a check further up.
func TestAStrangerCannotRateSomebodyElsesReport(t *testing.T) {
	svc, owner, stranger, _, _ := fixture(t)
	ctx := context.Background()

	item, err := svc.RequestGenerate(ctx, owner.ID, time.Time{})
	if err != nil {
		t.Fatal(err)
	}

	yes := true
	if _, err = svc.SetHelpful(ctx, item.ID, stranger.ID, &yes); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("a stranger's rating returned %v, want not-found", err)
	}

	reloaded, err := svc.Get(ctx, item.ID, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Rated() {
		t.Error("the stranger's rating was stored anyway")
	}
}

func TestRatingAReportThatDoesNotExist(t *testing.T) {
	svc, owner, _, _, _ := fixture(t)

	yes := true
	if _, err := svc.SetHelpful(context.Background(), uuid.New(), owner.ID, &yes); !errors.Is(err, apperr.ErrNotFound) {
		t.Fatalf("got %v, want not-found", err)
	}
}
