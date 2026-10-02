package nudges_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/decisions/decision"
	"github.com/NorthAIProject/north-client/internal/nudges"
)

type fakeDecisions struct{ due *decision.Revisit }

func (f fakeDecisions) DueRevisit(context.Context, uuid.UUID, time.Time) (decision.Revisit, bool, error) {
	if f.due == nil {
		return decision.Revisit{}, false, nil
	}
	return *f.due, true, nil
}

// At ten in the morning, one question about the due decision, once.
func TestDecisionRevisitAsksOnce(t *testing.T) {
	ctx := context.Background()
	morning := time.Date(2026, 10, 2, 10, 5, 0, 0, time.UTC)
	user, svc, prefs, _ := eveningUser(t, "revisit-nudge@north.test", morning)
	eveningPrefs(t, prefs, user, false)
	id := uuid.MustParse("66666666-aaaa-aaaa-aaaa-666666666666")
	svc = svc.WithDecisions(fakeDecisions{due: &decision.Revisit{ID: id, Title: "Run the half in March", Mark: 30}})

	if _, err := svc.Evaluate(ctx, user); err != nil {
		t.Fatal(err)
	}
	list, _ := svc.ListOpen(ctx, user.ID, 10)
	var found int
	for _, n := range list {
		if n.Kind == nudges.KindDecisionRevisit {
			found++
			if n.Href != "/app/decisions/"+id.String() {
				t.Fatalf("href = %q", n.Href)
			}
		}
	}
	if found != 1 {
		t.Fatalf("revisit nudges = %d (open: %v)", found, openKinds(t, svc, user))
	}
	if _, err := svc.Evaluate(ctx, user); err != nil {
		t.Fatal(err)
	}
	if kinds := openKinds(t, svc, user); count(kinds, nudges.KindDecisionRevisit) != 1 {
		t.Fatalf("asked twice: %v", kinds)
	}
}

// Outside its hour, nothing.
func TestDecisionRevisitKeepsToTheMorning(t *testing.T) {
	ctx := context.Background()
	afternoon := time.Date(2026, 10, 2, 15, 5, 0, 0, time.UTC)
	user, svc, prefs, _ := eveningUser(t, "revisit-afternoon@north.test", afternoon)
	eveningPrefs(t, prefs, user, false)
	svc = svc.WithDecisions(fakeDecisions{due: &decision.Revisit{ID: uuid.New(), Title: "x", Mark: 30}})
	if _, err := svc.Evaluate(ctx, user); err != nil {
		t.Fatal(err)
	}
	if kinds := openKinds(t, svc, user); count(kinds, nudges.KindDecisionRevisit) != 0 {
		t.Fatalf("asked in the afternoon: %v", kinds)
	}
}

func count(list []string, want string) int {
	n := 0
	for _, s := range list {
		if s == want {
			n++
		}
	}
	return n
}
