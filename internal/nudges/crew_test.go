package nudges_test

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/crews"
	"github.com/NorthAIProject/north-client/internal/nudges"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
)

// A crewmate checked in and you have not: one note in the evening, naming
// them, with the mood buttons. Not before the evening, not twice.
func TestCrewCheckInNudge(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	evening := time.Date(2026, 9, 30, 19, 0, 0, 0, time.UTC)
	ana := mustOnboard(t, pool, seedUser(t, pool, "ana-crew@north.test"), evening.AddDate(0, 0, -30))
	leo := mustOnboard(t, pool, seedUser(t, pool, "leo-crew@north.test"), evening.AddDate(0, 0, -30))

	crewSvc := crews.NewService(pool, crews.CheckInsFrom(checkins.NewService(checkins.NewRepository(pool), nil)), nil)
	c, err := crewSvc.Create(ctx, ana.ID, "Duo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = crewSvc.Join(ctx, leo.ID, c.Code); err != nil {
		t.Fatal(err)
	}
	writeCheckIn(t, pool, leo.ID, time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC))

	if n, err := evalService(pool, evening.Add(-3*time.Hour)).WithCrews(crewSvc).Evaluate(ctx, ana); err != nil || n != 0 {
		t.Fatalf("afternoon created %d, %v; want nothing before the evening", n, err)
	}
	svc := evalService(pool, evening).WithCrews(crewSvc)
	if n, err := svc.Evaluate(ctx, ana); err != nil || n != 1 {
		t.Fatalf("evening created %d, %v (open %v)", n, err, openKinds(t, svc, ana))
	}
	list, _ := svc.ListOpen(ctx, ana.ID, 10)
	if len(list) != 1 || list[0].Kind != nudges.KindCrewCheckIn || list[0].Title != "Test checked in today" {
		t.Fatalf("open = %#v", list)
	}
	if nudges.PushCategory(list[0].Kind) != nudges.CategoryCheckIn {
		t.Fatal("no mood buttons on the crew note")
	}
	if n, _ := evalService(pool, evening.Add(time.Hour)).WithCrews(crewSvc).Evaluate(ctx, ana); n != 0 {
		t.Fatalf("second sweep created %d", n)
	}
}
