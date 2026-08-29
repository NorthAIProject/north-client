package memories_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/memories"
	"github.com/NorthAIProject/north-client/internal/memories/extract"
	"github.com/NorthAIProject/north-client/internal/memories/memory"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
)

// believe creates a fact the coach can already see.
func believe(t *testing.T, svc *memories.Service, userID uuid.UUID, category, content string) memories.Memory {
	t.Helper()

	m, err := svc.Create(context.Background(), userID, memories.Input{
		Category: category,
		Content:  content,
	})
	if err != nil {
		t.Fatalf("create %q: %v", content, err)
	}
	if !m.IsApproved() {
		t.Fatalf("create %q produced status %q, want approved", content, m.Status)
	}
	return m
}

// propose files a pending fact that claims to replace another.
func propose(t *testing.T, svc *memories.Service, userID uuid.UUID, content string, supersedes *uuid.UUID) memories.Memory {
	t.Helper()

	ctx := context.Background()
	n, err := svc.InsertExtractions(ctx, userID, uuid.Nil, []memories.Proposal{{
		Candidate:    extract.Candidate{Category: memory.CategoryHabit, Content: content, Confidence: 0.9},
		SupersedesID: supersedes,
	}})
	if err != nil || n != 1 {
		t.Fatalf("propose %q: n=%d err=%v", content, n, err)
	}

	pending, err := svc.ListPending(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range pending {
		if m.Content == content {
			return m
		}
	}
	t.Fatalf("proposed %q but it is not pending", content)
	return memories.Memory{}
}

func contextContents(t *testing.T, svc *memories.Service, userID uuid.UUID) []string {
	t.Helper()

	got, err := svc.ForContext(context.Background(), userID, "")
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(got))
	for _, r := range got {
		out = append(out, r.Content)
	}
	return out
}

func contains(items []string, want string) bool {
	for _, it := range items {
		if it == want {
			return true
		}
	}
	return false
}

func newSvc(t *testing.T) (*memories.Service, *pgxpool.Pool) {
	t.Helper()
	pool := testdb.New(t)
	return memories.NewService(memories.NewRepository(pool)), pool
}

// The headline behaviour: two contradicting facts must never both reach the
// coach. Before this existed, "trains five days a week" and "trains three days a
// week" were both approved, both current, and the coach had to guess.
func TestApprovingAReplacementRetiresTheFactItReplaces(t *testing.T) {
	svc, pool := newSvc(t)
	ctx := context.Background()
	user := seedUser(t, pool, "supersede-happy@north.test")

	old := believe(t, svc, user.ID, memories.CategoryHabit, "Trains five days a week")
	replacement := propose(t, svc, user.ID, "Trains three days a week", &old.ID)

	if got := contextContents(t, svc, user.ID); !contains(got, old.Content) {
		t.Fatalf("the old fact should reach the coach before the replacement is approved, got %v", got)
	}

	if _, err := svc.Approve(ctx, replacement.ID, user.ID); err != nil {
		t.Fatal(err)
	}

	reloaded, err := svc.Get(ctx, old.ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.IsCurrent() {
		t.Error("the replaced fact is still current")
	}
	if reloaded.ValidTo == nil {
		t.Error("the replaced fact has no valid_to")
	}

	got := contextContents(t, svc, user.ID)
	if contains(got, old.Content) {
		t.Errorf("the retired fact still reaches the coach: %v", got)
	}
	if !contains(got, replacement.Content) {
		t.Errorf("the replacement does not reach the coach: %v", got)
	}
}

// A retired fact is history, not rubbish. The review page still shows it,
// because "they used to train five days a week" is worth knowing — it just stops
// being presented as currently true.
func TestARetiredFactIsStillVisibleToThePerson(t *testing.T) {
	svc, pool := newSvc(t)
	ctx := context.Background()
	user := seedUser(t, pool, "supersede-history@north.test")

	old := believe(t, svc, user.ID, memories.CategoryHabit, "Trains five days a week")
	replacement := propose(t, svc, user.ID, "Trains three days a week", &old.ID)
	if _, err := svc.Approve(ctx, replacement.ID, user.ID); err != nil {
		t.Fatal(err)
	}

	list, err := svc.List(ctx, user.ID, 50)
	if err != nil {
		t.Fatal(err)
	}

	var found bool
	for _, m := range list {
		if m.ID == old.ID {
			found = true
			if !m.IsSuperseded() {
				t.Error("the listed fact does not report itself superseded")
			}
		}
	}
	if !found {
		t.Error("the retired fact vanished from the person's own list")
	}
}

// Rejecting the replacement must leave the old fact alone. This is the whole
// reason supersession is applied on approval rather than at extraction: a model
// proposing a replacement is not evidence that the old fact is false.
func TestRejectingAReplacementLeavesTheOldFactAlone(t *testing.T) {
	svc, pool := newSvc(t)
	ctx := context.Background()
	user := seedUser(t, pool, "supersede-reject@north.test")

	old := believe(t, svc, user.ID, memories.CategoryHabit, "Trains five days a week")
	replacement := propose(t, svc, user.ID, "Trains three days a week", &old.ID)

	if _, err := svc.Reject(ctx, replacement.ID, user.ID); err != nil {
		t.Fatal(err)
	}

	reloaded, err := svc.Get(ctx, old.ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.IsCurrent() {
		t.Error("rejecting the replacement retired the old fact anyway")
	}
	if got := contextContents(t, svc, user.ID); !contains(got, old.Content) {
		t.Errorf("the old fact stopped reaching the coach: %v", got)
	}
}

// A pinned fact is one the person said always matters. A model's suggestion that
// it is out of date is not enough to retire it — the replacement still lands for
// a human to look at, and both stay visible.
func TestAPinnedFactIsNeverRetiredAutomatically(t *testing.T) {
	svc, pool := newSvc(t)
	ctx := context.Background()
	user := seedUser(t, pool, "supersede-pinned@north.test")

	old := believe(t, svc, user.ID, memories.CategoryInjury, "Cannot press overhead, shoulder instability")
	if _, err := svc.SetPinned(ctx, old.ID, user.ID, true); err != nil {
		t.Fatal(err)
	}

	replacement := propose(t, svc, user.ID, "The shoulder is fully recovered now", &old.ID)

	// Approval must still succeed. Refusing the whole approval because the
	// target is pinned would lose the new fact as well, which is worse.
	if _, err := svc.Approve(ctx, replacement.ID, user.ID); err != nil {
		t.Fatalf("approval failed because the target was pinned: %v", err)
	}

	reloaded, err := svc.Get(ctx, old.ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reloaded.IsCurrent() {
		t.Error("a pinned fact was retired on a model's suggestion")
	}

	got := contextContents(t, svc, user.ID)
	if !contains(got, old.Content) || !contains(got, replacement.Content) {
		t.Errorf("both facts should reach the coach for a human to resolve, got %v", got)
	}
}

// Approving something twice must not move a date already set, or the record of
// when a fact stopped being true would drift every time somebody clicked.
func TestApprovingTwiceDoesNotMoveTheRetirementDate(t *testing.T) {
	svc, pool := newSvc(t)
	ctx := context.Background()
	user := seedUser(t, pool, "supersede-idempotent@north.test")

	old := believe(t, svc, user.ID, memories.CategoryHabit, "Trains five days a week")
	replacement := propose(t, svc, user.ID, "Trains three days a week", &old.ID)

	if _, err := svc.Approve(ctx, replacement.ID, user.ID); err != nil {
		t.Fatal(err)
	}
	first, err := svc.Get(ctx, old.ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err = svc.Approve(ctx, replacement.ID, user.ID); err != nil {
		t.Fatalf("second approval errored: %v", err)
	}
	second, err := svc.Get(ctx, old.ID, user.ID)
	if err != nil {
		t.Fatal(err)
	}

	if !first.ValidTo.Equal(*second.ValidTo) {
		t.Errorf("valid_to moved from %v to %v on a second approval", first.ValidTo, second.ValidTo)
	}
}

// A target that has since been deleted is not an error. The new fact still
// stands; there is simply nothing left to retire.
func TestApprovingAReplacementForADeletedFactStillSucceeds(t *testing.T) {
	svc, pool := newSvc(t)
	ctx := context.Background()
	user := seedUser(t, pool, "supersede-deleted@north.test")

	old := believe(t, svc, user.ID, memories.CategoryHabit, "Trains five days a week")
	replacement := propose(t, svc, user.ID, "Trains three days a week", &old.ID)

	if err := svc.Delete(ctx, old.ID, user.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Approve(ctx, replacement.ID, user.ID); err != nil {
		t.Fatalf("approval failed after the target was deleted: %v", err)
	}
	if got := contextContents(t, svc, user.ID); !contains(got, replacement.Content) {
		t.Errorf("the replacement does not reach the coach: %v", got)
	}
}

// People revert. Someone who goes back to five days a week must be able to have
// that fact proposed again, which means the exact-text dedupe cannot count
// retired facts as already known.
func TestARetiredFactCanBeProposedAgain(t *testing.T) {
	svc, pool := newSvc(t)
	ctx := context.Background()
	user := seedUser(t, pool, "supersede-revert@north.test")

	old := believe(t, svc, user.ID, memories.CategoryHabit, "Trains five days a week")
	replacement := propose(t, svc, user.ID, "Trains three days a week", &old.ID)
	if _, err := svc.Approve(ctx, replacement.ID, user.ID); err != nil {
		t.Fatal(err)
	}

	n, err := svc.InsertExtractions(ctx, user.ID, uuid.Nil, []memories.Proposal{{
		Candidate: extract.Candidate{
			Category:   memory.CategoryHabit,
			Content:    old.Content,
			Confidence: 0.9,
		},
		SupersedesID: &replacement.ID,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("re-proposing a retired fact inserted %d rows, want 1", n)
	}
}

// The believed list offered to an extraction must contain only what is actually
// believed: not pending facts, which were never agreed, and not retired ones,
// which are already gone. Offering either invites the model to supersede
// something that was never there.
func TestOnlyCurrentApprovedFactsAreOfferedForSupersession(t *testing.T) {
	svc, pool := newSvc(t)
	ctx := context.Background()
	user := seedUser(t, pool, "supersede-offered@north.test")

	current := believe(t, svc, user.ID, memories.CategoryHabit, "Trains five days a week")
	retired := believe(t, svc, user.ID, memories.CategoryHabit, "Runs on Sundays without fail")
	stillPending := propose(t, svc, user.ID, "Eats oats every morning without exception", nil)

	// Retire one of them through the normal path.
	replacement := propose(t, svc, user.ID, "Stopped running on Sundays entirely", &retired.ID)
	if _, err := svc.Approve(ctx, replacement.ID, user.ID); err != nil {
		t.Fatal(err)
	}

	offered, err := svc.CurrentForSupersession(ctx, user.ID, 50)
	if err != nil {
		t.Fatal(err)
	}

	byID := map[uuid.UUID]bool{}
	for _, f := range offered {
		byID[f.ID] = true
	}

	if !byID[current.ID] {
		t.Error("a current approved fact was not offered")
	}
	if byID[retired.ID] {
		t.Error("a retired fact was offered for supersession")
	}
	if byID[stillPending.ID] {
		t.Error("a pending fact was offered for supersession")
	}
}
