package crews_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/checkins"
	"github.com/NorthAIProject/north-client/internal/crews"
	"github.com/NorthAIProject/north-client/internal/crews/crew"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

// accounts is a fixed page of people for the sweep.
type accounts []users.User

func (a accounts) ListOnboarded(_ context.Context, after uuid.UUID, _ int) ([]users.User, error) {
	if after != uuid.Nil {
		return nil, nil
	}
	return a, nil
}

type met struct {
	user, crew uuid.UUID
	kind       string
	target     int
	weekStart  string
}

// recorder keeps what the sweep recorded.
type recorder struct{ got []met }

func (r *recorder) CrewChallengeMet(_ context.Context, userID, crewID uuid.UUID, _, kind string, target int, weekStart, _ time.Time) {
	r.got = append(r.got, met{userID, crewID, kind, target, weekStart.Format(time.DateOnly)})
}

// Monday 5 October 2026, 08:00 UTC: the week of 28 September has just closed.
var monday = time.Date(2026, 10, 5, 8, 0, 0, 0, time.UTC)

// On Monday morning each member who met their crew's challenge last week is
// recorded once; a member who fell short, a crew without a challenge, and a
// member whose Monday morning has not come yet are not.
func TestChallengeSweeperClosesTheWeek(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	cs := checkins.NewService(checkins.NewRepository(pool), nil)
	svc := crews.NewService(pool, crews.CheckInsFrom(cs), nil)

	ana := person(t, pool, "ana@north.test", "Ana")
	leo := person(t, pool, "leo@north.test", "Leo")
	// Mia is in Los Angeles, where it is 01:00: her Monday morning is later.
	mia := person(t, pool, "mia@north.test", "Mia")
	mia.Timezone = "America/Los_Angeles"

	c, err := svc.Create(ctx, ana.ID, "Runners")
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range []users.User{leo, mia} {
		if _, err = svc.Join(ctx, u.ID, c.Code); err != nil {
			t.Fatal(err)
		}
	}
	if err = svc.SetChallenge(ctx, c.ID, ana.ID, &crews.Challenge{Kind: crew.ChallengeCheckIns, Target: 3}); err != nil {
		t.Fatal(err)
	}
	// Set the week before, as the clock in this test sees it.
	if _, err = pool.Exec(ctx, `UPDATE crew_challenges SET created_at = $2 WHERE crew_id = $1`,
		c.ID, time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	// A second crew of Ana's, with no challenge, records nothing.
	if _, err = svc.Create(ctx, ana.ID, "Quiet"); err != nil {
		t.Fatal(err)
	}

	// Last week: Ana three check-ins, Leo two, Mia three.
	for _, d := range []int{0, 2, 6} {
		checkIn(t, pool, ana, time.Date(2026, 9, 28+d, 0, 0, 0, 0, time.UTC))
		checkIn(t, pool, mia, time.Date(2026, 9, 28+d, 0, 0, 0, 0, time.UTC))
	}
	for _, d := range []int{1, 3} {
		checkIn(t, pool, leo, time.Date(2026, 9, 28+d, 0, 0, 0, 0, time.UTC))
	}
	// This week's check-in does not count toward last week.
	checkIn(t, pool, leo, monday)

	rec := &recorder{}
	sweeper := crews.NewChallengeSweeper(accounts{ana, leo, mia}, svc, rec, nil)
	if err = sweeper.WithClock(func() time.Time { return monday }).HandleSweep(ctx, nil); err != nil {
		t.Fatal(err)
	}
	want := met{ana.ID, c.ID, crew.ChallengeCheckIns, 3, "2026-09-28"}
	if len(rec.got) != 1 || rec.got[0] != want {
		t.Fatalf("recorded %+v, want only %+v", rec.got, want)
	}

	// Tuesday the sweep does nothing.
	rec.got = nil
	if err = sweeper.WithClock(func() time.Time { return monday.AddDate(0, 0, 1) }).HandleSweep(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if len(rec.got) != 0 {
		t.Fatalf("Tuesday recorded %+v", rec.got)
	}
}

// A challenge set after the week closed did not exist during it.
func TestChallengeSweeperSkipsAChallengeSetAfterTheWeek(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	cs := checkins.NewService(checkins.NewRepository(pool), nil)
	svc := crews.NewService(pool, crews.CheckInsFrom(cs), nil)
	ana := person(t, pool, "ana@north.test", "Ana")

	c, err := svc.Create(ctx, ana.ID, "Late starters")
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.SetChallenge(ctx, c.ID, ana.ID, &crews.Challenge{Kind: crew.ChallengeCheckIns, Target: 1}); err != nil {
		t.Fatal(err)
	}
	// Set at 06:00 on Monday, after the week it would close had ended.
	if _, err = pool.Exec(ctx, `UPDATE crew_challenges SET created_at = $2 WHERE crew_id = $1`,
		c.ID, time.Date(2026, 10, 5, 6, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	checkIn(t, pool, ana, time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC))

	rec := &recorder{}
	if err = crews.NewChallengeSweeper(accounts{ana}, svc, rec, nil).
		WithClock(func() time.Time { return monday }).HandleSweep(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if len(rec.got) != 0 {
		t.Fatalf("recorded %+v for a challenge that did not exist that week", rec.got)
	}
}
