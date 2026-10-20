package crews_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/NorthAIProject/north-client/internal/users"
)

// grow adds n more members who accepted the terms.
func (r room) grow(t *testing.T, n int) []users.User {
	t.Helper()
	ctx := context.Background()
	var out []users.User
	for i := range n {
		u := handled(t, r.pool, fmt.Sprintf("m%d@north.test", i), fmt.Sprintf("M%d", i), fmt.Sprintf("m%d_x", i))
		if _, err := r.svc.Join(ctx, u.ID, r.crew.Code); err != nil {
			t.Fatal(err)
		}
		if err := r.mod.AcceptTerms(ctx, u.ID); err != nil {
			t.Fatal(err)
		}
		out = append(out, u)
	}
	return out
}

func (r room) visibleTo(t *testing.T, u users.User) []string {
	t.Helper()
	page, err := r.chat.List(context.Background(), r.crew.ID, r.general, u.ID, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	return bodies(page.Messages)
}

// In a crew of four, two reports hide a message for everyone; in a crew of
// six it takes three.
func TestCrewMessageAutoHide(t *testing.T) {
	ctx := context.Background()

	small := newRoom(t)
	small.grow(t, 1)
	m := small.send(t, small.ana, "spam spam")
	for _, u := range []users.User{small.leo, small.owner} {
		if err := small.chat.Report(ctx, small.crew.ID, m.ID, u.ID, "spam", ""); err != nil {
			t.Fatal(err)
		}
	}
	if got := small.visibleTo(t, small.ana); slices.Contains(got, "spam spam") {
		t.Fatalf("crew of 4, 2 reports: the author still sees %v", got)
	}

	big := newRoom(t)
	more := big.grow(t, 3)
	m = big.send(t, big.ana, "spam spam")
	for i, u := range []users.User{big.leo, big.owner, more[0]} {
		if err := big.chat.Report(ctx, big.crew.ID, m.ID, u.ID, "spam", ""); err != nil {
			t.Fatal(err)
		}
		hidden := !slices.Contains(big.visibleTo(t, more[1]), "spam spam")
		if hidden != (i == 2) {
			t.Fatalf("crew of 6, %d reports: hidden %v", i+1, hidden)
		}
	}
}

func TestReporterStopsSeeingReported(t *testing.T) {
	r := newRoom(t)
	m := r.send(t, r.ana, "meh")
	if err := r.chat.Report(context.Background(), r.crew.ID, m.ID, r.leo.ID, "other", "not nice"); err != nil {
		t.Fatal(err)
	}
	if got := r.visibleTo(t, r.leo); slices.Contains(got, "meh") {
		t.Fatalf("the reporter still sees it: %v", got)
	}
	if got := r.visibleTo(t, r.owner); !slices.Contains(got, "meh") {
		t.Fatalf("the owner lost it after one report: %v", got)
	}
	if err := r.chat.Report(context.Background(), r.crew.ID, m.ID, r.ana.ID, "spam", ""); fieldError(err, "report") == "" {
		t.Fatalf("reporting your own: %v", err)
	}
}
