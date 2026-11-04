package profiles_test

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/achievements"
	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/lifts"
	"github.com/NorthAIProject/north-client/internal/users"
)

// sets logs n sets of squats against the session, as its owner.
func (w world) sets(t *testing.T, u users.User, se activity.Session, n int) {
	t.Helper()
	svc := lifts.NewService(lifts.NewRepository(w.pool), nil).WithRecaps(w.acts, nil)
	at := se.StartedAt.Add(10 * time.Minute)
	for i := range n {
		if _, err := svc.Log(context.Background(), u, lifts.LogInput{
			ExerciseName: "Squat", SetNumber: i + 1, WeightKg: 100, Reps: 5, PerformedAt: &at, ActivitySessionID: &se.ID,
		}); err != nil {
			t.Fatal(err)
		}
	}
}

// Who may see a person's workout cards, on all three ways of reading them. The
// card is checked down to its sets, because they are read in a second query
// that has to be asked for the same person's rows as the first.
func TestWorkoutCardVisibility(t *testing.T) {
	type outcome int
	const (
		// visible: cards on the view, the page and the detail.
		visible outcome = iota
		// notShared: a profile with no workouts on it, and no detail.
		notShared
		// hidden: the whole profile is not found.
		hidden
	)
	cases := []struct {
		name  string
		setup func(t *testing.T, w world, owner, viewer users.User)
		self  bool
		want  outcome
	}{
		{name: "the owner", self: true, want: visible},
		{
			name: "the owner with training not shared",
			self: true,
			setup: func(t *testing.T, w world, owner, _ users.User) {
				w.share(t, owner, achievements.Sharing{Training: false})
			},
			want: visible,
		},
		{name: "a stranger when training is shared", want: visible},
		{
			name: "a follower when training is shared",
			setup: func(t *testing.T, w world, owner, viewer users.User) {
				t.Helper()
				ctx := context.Background()
				if _, err := w.soc.Follow(ctx, viewer.ID, "owner"); err != nil {
					t.Fatal(err)
				}
				if err := w.soc.Accept(ctx, owner.ID, viewer.ID); err != nil {
					t.Fatal(err)
				}
			},
			want: visible,
		},
		{
			name: "a stranger when training is not shared",
			setup: func(t *testing.T, w world, owner, _ users.User) {
				w.share(t, owner, achievements.Sharing{Training: false})
			},
			want: notShared,
		},
		{
			name: "someone the owner blocked",
			setup: func(t *testing.T, w world, owner, viewer users.User) {
				t.Helper()
				if err := w.soc.Block(context.Background(), owner.ID, viewer.ID); err != nil {
					t.Fatal(err)
				}
			},
			want: hidden,
		},
		{
			name: "someone who blocked the owner",
			setup: func(t *testing.T, w world, owner, viewer users.User) {
				t.Helper()
				if err := w.soc.Block(context.Background(), viewer.ID, owner.ID); err != nil {
					t.Fatal(err)
				}
			},
			want: hidden,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			ctx := context.Background()
			owner, other := person(t, w.pool, "owner@n.test", "Owner"), person(t, w.pool, "other@n.test", "Other")
			w.handle(t, owner, "owner")
			w.handle(t, other, "other")
			viewer := other
			if c.self {
				viewer = owner
			}
			newer, older := w.workout(t, owner, time.Hour), w.workout(t, owner, 30*time.Hour)
			w.sets(t, owner, newer, 2)
			w.sets(t, owner, older, 3)
			// Somebody else's workout, which is never on the owner's profile.
			theirs := w.workout(t, other, 2*time.Hour)
			w.sets(t, other, theirs, 4)
			if c.setup != nil {
				c.setup(t, w, owner, viewer)
			}

			view, viewErr := w.svc.View(ctx, viewer, "owner", now)
			page, pageErr := w.svc.Workouts(ctx, viewer, "owner", time.Time{})
			card, cardErr := w.svc.Workout(ctx, viewer, "owner", newer.ID)

			switch c.want {
			case hidden:
				if !isNotFound(viewErr) || !isNotFound(pageErr) || !isNotFound(cardErr) {
					t.Fatalf("view %v, page %v, detail %v; want all not found", viewErr, pageErr, cardErr)
				}
				return
			case notShared:
				if viewErr != nil || pageErr != nil {
					t.Fatalf("view %v, page %v", viewErr, pageErr)
				}
				if view.Workouts != nil || view.Recent != nil || page.Shared || len(page.Items) != 0 {
					t.Fatalf("workouts leaked: view %+v, page %+v", view, page)
				}
				if !isNotFound(cardErr) {
					t.Fatalf("detail err = %v", cardErr)
				}
				return
			}

			if viewErr != nil || pageErr != nil || cardErr != nil {
				t.Fatalf("view %v, page %v, detail %v", viewErr, pageErr, cardErr)
			}
			if view.Workouts == nil || *view.Workouts != 2 || len(view.Recent) != 2 {
				t.Fatalf("view = %+v", view)
			}
			if !page.Shared || len(page.Items) != 2 {
				t.Fatalf("page = %+v", page)
			}
			for label, cards := range map[string]struct{ newer, older int }{
				"view": {view.Recent[0].Sets, view.Recent[1].Sets},
				"page": {page.Items[0].Sets, page.Items[1].Sets},
			} {
				if cards.newer != 2 || cards.older != 3 {
					t.Fatalf("%s sets = %+v, want 2 and 3", label, cards)
				}
			}
			for _, got := range append(view.Recent, page.Items...) {
				if got.UserID != owner.ID {
					t.Fatalf("card of %v on the owner's profile", got.UserID)
				}
			}
			if card.ID != newer.ID || card.UserID != owner.ID || card.Sets != 2 {
				t.Fatalf("detail = %+v", card)
			}

			// Whoever the viewer is, a session is only readable through its
			// owner's handle.
			if _, err := w.svc.Workout(ctx, viewer, "owner", theirs.ID); !isNotFound(err) {
				t.Fatalf("another person's session through the owner's handle: %v", err)
			}
		})
	}
}
