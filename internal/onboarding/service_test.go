package onboarding_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/goals"
	"github.com/NorthAIProject/north-client/internal/memories"
	"github.com/NorthAIProject/north-client/internal/onboarding"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/shared/lifedomain"
	"github.com/NorthAIProject/north-client/internal/users"
)

func seedUser(t *testing.T, pool *pgxpool.Pool, email string) users.User {
	t.Helper()
	u, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        email,
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Test User",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !u.NeedsOnboarding() {
		t.Fatal("new user should need onboarding")
	}
	return u
}

func newSvc(pool *pgxpool.Pool) *onboarding.Service {
	return onboarding.NewService(
		users.NewService(users.NewRepository(pool)),
		memories.NewService(memories.NewRepository(pool)),
		goals.NewService(goals.NewRepository(pool)),
	)
}

func TestCompleteSeedsContext(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "onboard-complete@north.test")
	svc := newSvc(pool)

	updated, _, err := svc.Complete(ctx, user, onboarding.Answers{
		FocusAreas:    []string{lifedomain.Fitness, lifedomain.Health},
		CoachingStyle: onboarding.StyleText(onboarding.StyleDirect),
		NearTermGoal:  "Run a 10K",
		GoalCategory:  lifedomain.Fitness,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.NeedsOnboarding() {
		t.Fatal("expected onboarded user")
	}
	if updated.CoachingStyle != onboarding.StyleText(onboarding.StyleDirect) {
		t.Fatalf("coaching style = %q", updated.CoachingStyle)
	}

	userSvc := users.NewService(users.NewRepository(pool))
	reloaded, err := userSvc.ByID(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.OnboardedAt == nil {
		t.Fatal("onboarded_at not set in database")
	}

	memSvc := memories.NewService(memories.NewRepository(pool))
	memList, err := memSvc.ListApproved(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(memList) < 3 {
		t.Fatalf("expected at least 3 memories (2 focus + coaching), got %d", len(memList))
	}

	goalSvc := goals.NewService(goals.NewRepository(pool))
	active, err := goalSvc.ListActive(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Fatalf("active goals = %d", len(active))
	}
	if active[0].Title != "Run a 10K" {
		t.Fatalf("goal title = %q", active[0].Title)
	}
	if active[0].Category != lifedomain.Fitness {
		t.Fatalf("goal category = %q", active[0].Category)
	}

	ctxMems, err := memSvc.ForContext(ctx, user.ID, "fitness coaching")
	if err != nil {
		t.Fatal(err)
	}
	if len(ctxMems) == 0 {
		t.Fatal("expected seeded memories in coach context")
	}
}

func TestSkipMarksOnboardedOnly(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "onboard-skip@north.test")
	svc := newSvc(pool)

	updated, err := svc.Skip(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if updated.NeedsOnboarding() {
		t.Fatal("expected onboarded after skip")
	}

	memSvc := memories.NewService(memories.NewRepository(pool))
	memList, err := memSvc.ListApproved(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(memList) != 0 {
		t.Fatalf("skip should not seed memories, got %d", len(memList))
	}

	goalSvc := goals.NewService(goals.NewRepository(pool))
	active, err := goalSvc.ListActive(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 0 {
		t.Fatalf("skip should not seed goals, got %d", len(active))
	}
}

func TestCompleteIdempotent(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "onboard-idempotent@north.test")
	svc := newSvc(pool)

	answers := onboarding.Answers{
		FocusAreas:    []string{lifedomain.Work},
		CoachingStyle: onboarding.StyleText(onboarding.StyleSupportive),
		NearTermGoal:  "Ship the feature",
		GoalCategory:  lifedomain.Work,
	}

	first, _, err := svc.Complete(ctx, user, answers)
	if err != nil {
		t.Fatal(err)
	}

	second, _, err := svc.Complete(ctx, first, answers)
	if err != nil {
		t.Fatal(err)
	}
	if second.NeedsOnboarding() {
		t.Fatal("still onboarded after second complete")
	}

	goalSvc := goals.NewService(goals.NewRepository(pool))
	active, err := goalSvc.ListActive(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Fatalf("idempotent complete should not duplicate goals, got %d", len(active))
	}
}

// stubCoach records what onboarding asked the coach to do, without an AI
// provider or a context builder behind it.
type stubCoach struct {
	thread    uuid.UUID
	sent      string
	startErr  error
	sendErr   error
	sendCalls int
}

func (c *stubCoach) StartConversation(_ context.Context, _ uuid.UUID) (conversations.Conversation, error) {
	if c.startErr != nil {
		return conversations.Conversation{}, c.startErr
	}
	c.thread = uuid.New()
	return conversations.Conversation{ID: c.thread}, nil
}

func (c *stubCoach) SendMessage(_ context.Context, _ users.User, id uuid.UUID, text string) (<-chan ai.StreamChunk, error) {
	c.sendCalls++
	c.sent = text
	if c.sendErr != nil {
		return nil, c.sendErr
	}
	ch := make(chan ai.StreamChunk)
	close(ch)
	_ = id
	return ch, nil
}

// The cold start is the thing NOR-17 exists to remove: finishing the
// questionnaire and landing on an empty chat box.
func TestCompleteOpensTheFirstConversation(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "onboard-thread@north.test")

	coach := &stubCoach{}
	svc := newSvc(pool).WithCoach(coach, nil)

	_, thread, err := svc.Complete(ctx, user, onboarding.Answers{
		FocusAreas:    []string{lifedomain.Fitness},
		CoachingStyle: onboarding.StyleText(onboarding.StyleDirect),
		NearTermGoal:  "Run a 10K",
		GoalCategory:  lifedomain.Fitness,
	})
	if err != nil {
		t.Fatal(err)
	}
	if thread == uuid.Nil {
		t.Fatal("no conversation was opened")
	}
	if thread != coach.thread {
		t.Fatalf("returned %s, coach opened %s", thread, coach.thread)
	}
	if !strings.Contains(coach.sent, "Run a 10K") {
		t.Fatalf("opening message does not carry the goal: %q", coach.sent)
	}
	if !strings.Contains(coach.sent, lifedomain.Fitness) {
		t.Fatalf("opening message does not carry the focus area: %q", coach.sent)
	}
	if !strings.Contains(coach.sent, "photo") || !strings.Contains(coach.sent, "what do you need to see") {
		t.Fatalf("opening message should ask for evidence: %q", coach.sent)
	}
}

// A provider having a bad minute must not cost somebody their onboarding. They
// answered three questions; that work is theirs whether or not a model replied.
func TestCompleteSurvivesACoachThatCannotAnswer(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "onboard-nocoach@north.test")

	coach := &stubCoach{startErr: errors.New("no provider configured")}
	svc := newSvc(pool).WithCoach(coach, nil)

	updated, thread, err := svc.Complete(ctx, user, onboarding.Answers{
		FocusAreas:    []string{lifedomain.Work},
		CoachingStyle: onboarding.StyleText(onboarding.StyleSupportive),
		NearTermGoal:  "Ship the feature",
		GoalCategory:  lifedomain.Work,
	})
	if err != nil {
		t.Fatalf("onboarding failed because the coach did: %v", err)
	}
	if updated.NeedsOnboarding() {
		t.Fatal("user was not marked onboarded")
	}
	if thread != uuid.Nil {
		t.Fatalf("reported a thread that was never opened: %s", thread)
	}

	goalSvc := goals.NewService(goals.NewRepository(pool))
	active, err := goalSvc.ListActive(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Fatalf("seeded goals = %d, want 1", len(active))
	}
}

// Without a coach wired the questionnaire still has to work end to end.
func TestCompleteWithoutACoachSeedsNoThread(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "onboard-plain@north.test")

	_, thread, err := newSvc(pool).Complete(ctx, user, onboarding.Answers{
		FocusAreas:    []string{lifedomain.Learning},
		CoachingStyle: onboarding.StyleText(onboarding.StyleSocratic),
		NearTermGoal:  "Finish the course",
		GoalCategory:  lifedomain.Learning,
	})
	if err != nil {
		t.Fatal(err)
	}
	if thread != uuid.Nil {
		t.Fatalf("thread = %s, want none", thread)
	}
}

func TestValidateAnswers(t *testing.T) {
	_, err := onboarding.ValidateAnswers(nil, "", "", "")
	if err == nil {
		t.Fatal("expected validation error for empty form")
	}

	answers, err := onboarding.ValidateAnswers(
		[]string{lifedomain.Personal},
		onboarding.StyleDirect,
		"",
		"Read twelve books",
	)
	if err != nil {
		t.Fatal(err)
	}
	if answers.CoachingStyle != onboarding.StyleText(onboarding.StyleDirect) {
		t.Fatalf("style = %q", answers.CoachingStyle)
	}
	if answers.GoalCategory != lifedomain.Personal {
		t.Fatalf("category = %q", answers.GoalCategory)
	}
}
