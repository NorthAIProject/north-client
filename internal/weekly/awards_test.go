package weekly_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/awards/award"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/weekly"
)

type checked struct {
	users []uuid.UUID
	areas [][]award.Area
}

func (c *checked) Check(_ context.Context, id uuid.UUID, _ time.Time, areas ...award.Area) {
	c.users = append(c.users, id)
	c.areas = append(c.areas, areas)
}

func TestSetFocusChecksGoalsBadgesOnlyOnSuccess(t *testing.T) {
	ctx := context.Background()
	pool, user, goalSvc := setup(t)
	rec := &checked{}
	svc := weekly.NewService(weekly.NewRepository(pool), goalSvc, &fakeReports{}).WithAwards(rec).WithClock(func() time.Time { return sunday })
	if _, err := svc.SetFocus(ctx, user, weekly.Input{Volume: "max"}); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("err = %v, want validation", err)
	}
	if len(rec.users) != 0 {
		t.Fatalf("checked after a failed write: %v", rec.users)
	}
	if _, err := svc.SetFocus(ctx, user, weekly.Input{Priorities: []string{"Run"}}); err != nil {
		t.Fatal(err)
	}
	if len(rec.users) != 1 || rec.users[0] != user.ID || rec.areas[0][0] != award.Goals {
		t.Fatalf("checks = %v %v", rec.users, rec.areas)
	}
}
