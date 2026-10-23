package progresschecks_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/awards/award"
	"github.com/NorthAIProject/north-client/internal/progresschecks"
	"github.com/NorthAIProject/north-client/internal/progresschecks/progresscheck"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type checked struct {
	users []uuid.UUID
	areas [][]award.Area
}

func (c *checked) Check(_ context.Context, id uuid.UUID, _ time.Time, areas ...award.Area) {
	c.users = append(c.users, id)
	c.areas = append(c.areas, areas)
}

func TestSaveChecksBodyBadgesOnlyOnSuccess(t *testing.T) {
	f := newFixture(t)
	f.recordBaseline(t)
	rec := &checked{}
	f.svc.WithAwards(rec)
	if _, err := f.svc.Save(f.ctx, f.user, progresschecks.Input{}, nil); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("err = %v, want validation", err)
	}
	if len(rec.users) != 0 {
		t.Fatalf("checked after a failed write: %v", rec.users)
	}
	if _, err := f.svc.Save(f.ctx, f.user, progresschecks.Input{
		Measurements: progresscheck.Measurements{WaistCm: ptr(96)},
	}, nil); err != nil {
		t.Fatal(err)
	}
	if len(rec.users) != 1 || rec.users[0] != f.user.ID || rec.areas[0][0] != award.Body {
		t.Fatalf("checks = %v %v", rec.users, rec.areas)
	}
}
