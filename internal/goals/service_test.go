package goals_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

func newService(t *testing.T) (*goals.Service, users.User) {
	svc, user, _ := newServiceWithSecondUser(t)
	return svc, user
}

// newServiceWithSecondUser returns two real accounts, so ownership tests use a
// genuine stranger rather than an id that merely is not the owner's.
func newServiceWithSecondUser(t *testing.T) (*goals.Service, users.User, users.User) {
	t.Helper()

	pool := testdb.New(t)
	userSvc := users.NewService(users.NewRepository(pool))

	create := func(email, name string) users.User {
		u, err := userSvc.Register(context.Background(), users.Registration{
			Email:        email,
			PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
			DisplayName:  name,
			Timezone:     "Europe/Lisbon",
		})
		if err != nil {
			t.Fatalf("create user %s: %v", email, err)
		}
		return u
	}

	return goals.NewService(goals.NewRepository(pool)),
		create("fernando@north.test", "Fernando Correia"),
		create("stranger@north.test", "Someone Else")
}

func validGoal() goals.Input {
	return goals.Input{
		Title:      "Run 10k without stopping",
		Motivation: "I want to stop feeling out of breath on the stairs.",
		Success:    "I run 10k at any pace without walking.",
		Category:   goals.CategoryFitness,
		TargetDate: time.Now().AddDate(0, 3, 0),
	}
}

func TestCreateAndFetch(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, user.ID, validGoal())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Status != goals.StatusActive {
		t.Fatalf("a new goal should be active, got %q", created.Status)
	}
	if !created.HasDeadline() {
		t.Fatal("the target date was not stored")
	}

	fetched, err := svc.Get(ctx, created.ID, user.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fetched.Title != created.Title {
		t.Fatalf("title = %q", fetched.Title)
	}
}

func TestGoalsAreScopedToTheirOwner(t *testing.T) {
	svc, user, stranger := newServiceWithSecondUser(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, user.ID, validGoal())
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	// Ownership is part of the query, so a stranger holding the exact goal id
	// still finds nothing.
	if _, err = svc.Get(ctx, created.ID, stranger.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("a stranger should not reach this goal, got %v", err)
	}

	list, err := svc.List(ctx, stranger.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("a stranger sees %d of someone else's goals", len(list))
	}

	// And cannot change it.
	if _, err := svc.SetStatus(ctx, created.ID, stranger.ID, goals.StatusAbandoned); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("a stranger should not be able to close this goal, got %v", err)
	}
	if err := svc.Delete(ctx, created.ID, stranger.ID); err != nil {
		t.Fatalf("delete by a stranger should be a no-op, got %v", err)
	}
	if _, err := svc.Get(ctx, created.ID, user.ID); err != nil {
		t.Fatalf("the goal should still exist for its owner: %v", err)
	}
}

func TestValidationRejectsBadInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		field string
		in    goals.Input
	}{
		{"no title", "title", goals.Input{Category: goals.CategoryFitness}},
		{"unknown category", "category", goals.Input{Title: "x", Category: "interpretive dance"}},
		{
			// Almost always a mistyped year, and storing it makes the goal
			// permanently overdue from the moment it is created.
			"deadline in the past", "target_date",
			goals.Input{Title: "x", Category: goals.CategoryFitness, TargetDate: time.Now().AddDate(0, 0, -30)},
		},
		{"title too long", "title", goals.Input{Title: strings.Repeat("a", 201), Category: goals.CategoryOther}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := goals.Validate(tt.in)
			if err == nil {
				t.Fatalf("%s should be rejected", tt.name)
			}

			var fieldErrs apperr.FieldErrors
			if !apperr.As(err, &fieldErrs) {
				t.Fatalf("expected field errors, got %T", err)
			}
			if _, ok := fieldErrs.Messages()[tt.field]; !ok {
				t.Fatalf("expected the failure on %q, got %v", tt.field, fieldErrs.Messages())
			}
		})
	}
}

func TestAnEmptyCategoryDefaultsRatherThanFailing(t *testing.T) {
	t.Parallel()

	clean, err := goals.Validate(goals.Input{Title: "Read more"})
	if err != nil {
		t.Fatalf("a goal without a category should be accepted: %v", err)
	}
	if clean.Category != goals.CategoryOther {
		t.Fatalf("category = %q, want other", clean.Category)
	}
}

func TestOpenEndedGoalsAreAllowed(t *testing.T) {
	svc, user := newService(t)

	in := validGoal()
	in.TargetDate = time.Time{}

	created, err := svc.Create(context.Background(), user.ID, in)
	if err != nil {
		t.Fatalf("a goal with no deadline should be accepted: %v", err)
	}
	if created.HasDeadline() {
		t.Fatal("no deadline was given but one was stored")
	}
	if created.Overdue() {
		t.Fatal("a goal with no deadline can never be overdue")
	}
	if created.Deadline() != "No deadline" {
		t.Fatalf("deadline reads %q", created.Deadline())
	}
}

func TestStatusTransitionsStampAndClearClosedAt(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	created, _ := svc.Create(ctx, user.ID, validGoal())

	achieved, err := svc.SetStatus(ctx, created.ID, user.ID, goals.StatusAchieved)
	if err != nil {
		t.Fatalf("set achieved: %v", err)
	}
	if achieved.ClosedAt == nil {
		t.Fatal("finishing a goal should record when")
	}

	// Reopening must clear it, or a reactivated goal still looks finished.
	reopened, err := svc.SetStatus(ctx, created.ID, user.ID, goals.StatusActive)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if reopened.ClosedAt != nil {
		t.Fatal("reopening a goal should clear its closed date")
	}
}

func TestUnknownStatusIsRejected(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	created, _ := svc.Create(ctx, user.ID, validGoal())

	if _, err := svc.SetStatus(ctx, created.ID, user.ID, "vibing"); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

// goal_updates carries its own user_id, so without an ownership check the
// service would happily attach a note to someone else's goal.
func TestUpdatesCannotBeAddedToAnotherUsersGoal(t *testing.T) {
	svc, user, stranger := newServiceWithSecondUser(t)
	ctx := context.Background()

	created, _ := svc.Create(ctx, user.ID, validGoal())

	if _, err := svc.AddUpdate(ctx, created.ID, stranger.ID, "sneaking in", nil); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("a note against another user's goal must be refused, got %v", err)
	}

	updates, err := svc.Updates(ctx, created.ID, user.ID, 10)
	if err != nil {
		t.Fatalf("updates: %v", err)
	}
	if len(updates) != 0 {
		t.Fatalf("the note was written anyway: %d", len(updates))
	}
}

func TestProgressNotesAttachAndSurfaceOnTheGoal(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	created, _ := svc.Create(ctx, user.ID, validGoal())

	progress := 40
	if _, err := svc.AddUpdate(ctx, created.ID, user.ID, "Ran 5k three times this week.", &progress); err != nil {
		t.Fatalf("add update: %v", err)
	}

	updates, err := svc.Updates(ctx, created.ID, user.ID, 10)
	if err != nil {
		t.Fatalf("updates: %v", err)
	}
	if len(updates) != 1 {
		t.Fatalf("expected one note, got %d", len(updates))
	}
	if updates[0].Progress == nil || *updates[0].Progress != 40 {
		t.Fatalf("progress = %v", updates[0].Progress)
	}

	// The list attaches the latest note so the coach and the index can show it
	// without a query per goal.
	list, err := svc.ListActive(ctx, user.ID)
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(list) != 1 || list[0].LatestUpdate == nil {
		t.Fatal("the latest note was not attached to the goal")
	}
	if !strings.Contains(list[0].LatestUpdate.Note, "Ran 5k") {
		t.Fatalf("wrong note attached: %q", list[0].LatestUpdate.Note)
	}
}

func TestProgressOutsideZeroToOneHundredIsRejected(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	created, _ := svc.Create(ctx, user.ID, validGoal())

	for _, bad := range []int{-1, 101} {
		if _, err := svc.AddUpdate(ctx, created.ID, user.ID, "note", &bad); err == nil {
			t.Errorf("progress %d should be rejected", bad)
		}
	}
}

func TestListActiveExcludesFinishedGoals(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	keep, _ := svc.Create(ctx, user.ID, validGoal())

	second := validGoal()
	second.Title = "Learn to swim"
	done, _ := svc.Create(ctx, user.ID, second)

	if _, err := svc.SetStatus(ctx, done.ID, user.ID, goals.StatusAchieved); err != nil {
		t.Fatalf("set achieved: %v", err)
	}

	active, err := svc.ListActive(ctx, user.ID)
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(active) != 1 || active[0].ID != keep.ID {
		t.Fatalf("expected only the active goal, got %d", len(active))
	}

	// The full list still shows everything, so a finished goal is not lost.
	all, err := svc.List(ctx, user.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected both goals in the full list, got %d", len(all))
	}
	// Active sorts first, because that is what the person is actually doing.
	if all[0].ID != keep.ID {
		t.Fatal("active goals should sort above finished ones")
	}
}

func TestSummaryCarriesWhatTheCoachNeeds(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	created, _ := svc.Create(ctx, user.ID, validGoal())
	if _, err := svc.AddUpdate(ctx, created.ID, user.ID, "Managed 7k on Sunday.", nil); err != nil {
		t.Fatalf("add update: %v", err)
	}

	list, _ := svc.ListActive(ctx, user.ID)
	summary := list[0].Summary()

	// The summary is the only form the model ever sees, so everything that
	// should influence coaching has to survive into it.
	for _, want := range []string{"Run 10k", "because", "done when", "Managed 7k"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary is missing %q:\n%s", want, summary)
		}
	}
}

func TestMilestonesCanBeAddedEditedCompletedAndRemoved(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	created, _ := svc.Create(ctx, user.ID, validGoal())

	first, err := svc.AddMilestone(ctx, created.ID, user.ID, goals.MilestoneInput{Title: "Run 5k"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if first.Status != goals.MilestoneOpen {
		t.Fatalf("a new milestone should be open, got %q", first.Status)
	}
	if first.Position != 0 {
		t.Fatalf("first position = %d", first.Position)
	}

	second, err := svc.AddMilestone(ctx, created.ID, user.ID, goals.MilestoneInput{
		Title:      "Run 8k",
		TargetDate: time.Now().AddDate(0, 1, 0),
	})
	if err != nil {
		t.Fatalf("add second: %v", err)
	}
	if second.Position != 1 {
		t.Fatalf("second position = %d, want 1", second.Position)
	}

	renamed, err := svc.UpdateMilestone(ctx, first.ID, user.ID, goals.MilestoneInput{Title: "Run 5k without stopping"})
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	if renamed.Title != "Run 5k without stopping" {
		t.Fatalf("title = %q", renamed.Title)
	}

	done, err := svc.SetMilestoneStatus(ctx, first.ID, user.ID, goals.MilestoneCompleted)
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if !done.IsComplete() || done.CompletedAt == nil {
		t.Fatal("completing a milestone should stamp when")
	}

	reopened, err := svc.SetMilestoneStatus(ctx, first.ID, user.ID, goals.MilestoneOpen)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if reopened.IsComplete() || reopened.CompletedAt != nil {
		t.Fatal("reopening a milestone should clear its completed date")
	}

	if err := svc.DeleteMilestone(ctx, second.ID, user.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	left, err := svc.Milestones(ctx, created.ID, user.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(left) != 1 || left[0].ID != first.ID {
		t.Fatalf("expected only the first milestone, got %d", len(left))
	}
}

func TestMilestonesCannotBeTouchedByAnotherUser(t *testing.T) {
	svc, user, stranger := newServiceWithSecondUser(t)
	ctx := context.Background()

	created, _ := svc.Create(ctx, user.ID, validGoal())
	ms, err := svc.AddMilestone(ctx, created.ID, user.ID, goals.MilestoneInput{Title: "Run 5k"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}

	if _, err := svc.AddMilestone(ctx, created.ID, stranger.ID, goals.MilestoneInput{Title: "sneaking in"}); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("a stranger should not add a milestone, got %v", err)
	}
	if _, err := svc.UpdateMilestone(ctx, ms.ID, stranger.ID, goals.MilestoneInput{Title: "hijacked"}); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("a stranger should not edit a milestone, got %v", err)
	}
	if _, err := svc.SetMilestoneStatus(ctx, ms.ID, stranger.ID, goals.MilestoneCompleted); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("a stranger should not complete a milestone, got %v", err)
	}
	if err := svc.DeleteMilestone(ctx, ms.ID, stranger.ID); err != nil {
		t.Fatalf("delete by a stranger should be a no-op, got %v", err)
	}

	listed, err := svc.Milestones(ctx, created.ID, stranger.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("a stranger sees %d of someone else's milestones", len(listed))
	}

	still, err := svc.Milestones(ctx, created.ID, user.ID)
	if err != nil {
		t.Fatalf("owner list: %v", err)
	}
	if len(still) != 1 {
		t.Fatalf("the milestone should still exist for its owner, got %d", len(still))
	}
}

func TestProgressPrefersMilestonesOverNotePercentage(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	created, _ := svc.Create(ctx, user.ID, validGoal())

	progress := 40
	if _, err := svc.AddUpdate(ctx, created.ID, user.ID, "Halfway-ish.", &progress); err != nil {
		t.Fatalf("add update: %v", err)
	}

	list, err := svc.ListActive(ctx, user.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected one goal, got %d", len(list))
	}
	if pct, ok := list[0].Progress(); !ok || pct != 40 {
		t.Fatalf("note progress = %d ok=%v, want 40", pct, ok)
	}
	if list[0].ProgressLabel() != "40%" {
		t.Fatalf("label = %q", list[0].ProgressLabel())
	}

	if _, err := svc.AddMilestone(ctx, created.ID, user.ID, goals.MilestoneInput{Title: "Run 5k"}); err != nil {
		t.Fatalf("add first: %v", err)
	}
	second, err := svc.AddMilestone(ctx, created.ID, user.ID, goals.MilestoneInput{Title: "Run 8k"})
	if err != nil {
		t.Fatalf("add second: %v", err)
	}
	if _, err := svc.AddMilestone(ctx, created.ID, user.ID, goals.MilestoneInput{Title: "Run 10k"}); err != nil {
		t.Fatalf("add third: %v", err)
	}
	if _, err := svc.SetMilestoneStatus(ctx, second.ID, user.ID, goals.MilestoneCompleted); err != nil {
		t.Fatalf("complete: %v", err)
	}

	list, err = svc.ListActive(ctx, user.ID)
	if err != nil {
		t.Fatalf("list after milestones: %v", err)
	}
	if pct, ok := list[0].Progress(); !ok || pct != 33 {
		t.Fatalf("milestone progress = %d ok=%v, want 33", pct, ok)
	}
	if list[0].ProgressLabel() != "1 of 3" {
		t.Fatalf("label = %q", list[0].ProgressLabel())
	}
}

func TestCompletingEveryMilestoneLeavesTheGoalActive(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	created, _ := svc.Create(ctx, user.ID, validGoal())
	ms, err := svc.AddMilestone(ctx, created.ID, user.ID, goals.MilestoneInput{Title: "Run 5k"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := svc.SetMilestoneStatus(ctx, ms.ID, user.ID, goals.MilestoneCompleted); err != nil {
		t.Fatalf("complete: %v", err)
	}

	fetched, err := svc.Get(ctx, created.ID, user.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if fetched.Status != goals.StatusActive {
		t.Fatalf("status = %q, want still active", fetched.Status)
	}
}

func TestDeleteRemovesMilestonesWithTheGoal(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	created, _ := svc.Create(ctx, user.ID, validGoal())
	if _, err := svc.AddMilestone(ctx, created.ID, user.ID, goals.MilestoneInput{Title: "Run 5k"}); err != nil {
		t.Fatalf("add: %v", err)
	}

	if err := svc.Delete(ctx, created.ID, user.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	left, err := svc.Milestones(ctx, created.ID, user.ID)
	if err != nil {
		t.Fatalf("milestones: %v", err)
	}
	if len(left) != 0 {
		t.Fatalf("expected milestones to cascade, got %d", len(left))
	}
}

func TestMilestoneValidationRejectsBadInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		field string
		in    goals.MilestoneInput
	}{
		{"no title", "title", goals.MilestoneInput{}},
		{"title too long", "title", goals.MilestoneInput{Title: strings.Repeat("a", 201)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := goals.ValidateMilestone(tt.in)
			if err == nil {
				t.Fatalf("%s should be rejected", tt.name)
			}
			var fieldErrs apperr.FieldErrors
			if !apperr.As(err, &fieldErrs) {
				t.Fatalf("expected field errors, got %T", err)
			}
			if _, ok := fieldErrs.Messages()[tt.field]; !ok {
				t.Fatalf("expected the failure on %q, got %v", tt.field, fieldErrs.Messages())
			}
		})
	}
}

func TestUnknownMilestoneStatusIsRejected(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	created, _ := svc.Create(ctx, user.ID, validGoal())
	ms, _ := svc.AddMilestone(ctx, created.ID, user.ID, goals.MilestoneInput{Title: "Run 5k"})

	if _, err := svc.SetMilestoneStatus(ctx, ms.ID, user.ID, "vibing"); !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("expected ErrValidation, got %v", err)
	}
}

func TestUpdatesAreScopedToTheOwner(t *testing.T) {
	svc, user, stranger := newServiceWithSecondUser(t)
	ctx := context.Background()

	created, _ := svc.Create(ctx, user.ID, validGoal())
	if _, err := svc.AddUpdate(ctx, created.ID, user.ID, "a private note", nil); err != nil {
		t.Fatalf("add update: %v", err)
	}

	seen, err := svc.Updates(ctx, created.ID, stranger.ID, 10)
	if err != nil {
		t.Fatalf("stranger updates: %v", err)
	}
	if len(seen) != 0 {
		t.Fatalf("a stranger sees %d of someone else's notes", len(seen))
	}
}

func TestSummaryIncludesMilestoneProgress(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	created, _ := svc.Create(ctx, user.ID, validGoal())
	ms, err := svc.AddMilestone(ctx, created.ID, user.ID, goals.MilestoneInput{Title: "Run 5k"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := svc.AddMilestone(ctx, created.ID, user.ID, goals.MilestoneInput{Title: "Run 10k"}); err != nil {
		t.Fatalf("add second: %v", err)
	}
	if _, err := svc.SetMilestoneStatus(ctx, ms.ID, user.ID, goals.MilestoneCompleted); err != nil {
		t.Fatalf("complete: %v", err)
	}

	list, err := svc.ListActive(ctx, user.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	summary := list[0].Summary()
	if !strings.Contains(summary, "1 of 2 milestones") {
		t.Fatalf("summary is missing milestone progress:\n%s", summary)
	}
}

func TestDeleteRemovesTheGoalAndItsNotes(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	created, _ := svc.Create(ctx, user.ID, validGoal())
	if _, err := svc.AddUpdate(ctx, created.ID, user.ID, "a note", nil); err != nil {
		t.Fatalf("add update: %v", err)
	}

	if err := svc.Delete(ctx, created.ID, user.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, err := svc.Get(ctx, created.ID, user.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("the goal should be gone, got %v", err)
	}

	// The notes go with it, by cascade rather than by application code.
	updates, err := svc.Updates(ctx, created.ID, user.ID, 10)
	if err != nil {
		t.Fatalf("updates: %v", err)
	}
	if len(updates) != 0 {
		t.Fatalf("expected the notes to be deleted too, got %d", len(updates))
	}
}
