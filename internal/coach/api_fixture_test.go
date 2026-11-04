package coach_test

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/quota"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	"github.com/NorthAIProject/north-client/internal/users"
)

// countingRepo reports the first-page read a reply is not supposed to pay for.
type countingRepo struct {
	conversations.Repository
	firstPageReads atomic.Int32
}

func (r *countingRepo) Messages(ctx context.Context, conversationID uuid.UUID, limit int) ([]conversations.Message, error) {
	r.firstPageReads.Add(1)
	return r.Repository.Messages(ctx, conversationID, limit)
}

type threadFixture struct {
	harness
	repo   *countingRepo
	router http.Handler
}

// newThreadFixture mounts the API as cmd/api does, with the bearer check replaced
// by a fixed signed-in user.
func newThreadFixture(t *testing.T, client *fake.Client) threadFixture {
	t.Helper()

	pool := testdb.New(t)
	user, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        "long-thread@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Long Thread",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	registry := ai.NewRegistry()
	registry.Register(client)

	repo := &countingRepo{Repository: conversations.NewRepository(pool)}
	convos := conversations.NewService(repo)
	svc := coach.NewService(coach.Options{
		Registry:       registry,
		Conversations:  convos,
		ContextBuilder: coach.NewContextBuilder(convos),
		PromptBuilder:  coach.NewPromptBuilder(),
		Chains:         ai.NewChainSet([]string{client.Name()}, nil),
		Model:          "test-model",
		FastModel:      "test-fast-model",
	})

	e := echox.New(echox.Config{})
	g := e.Group("", func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			req := c.Request()
			c.SetRequest(req.WithContext(auth.ContextWithUser(req.Context(), user)))
			return next(c)
		}
	})
	// No budget configured, so nothing is refused and no counter is reached.
	coach.NewAPI(svc, quota.NewService(nil, quota.NewLimits(nil, nil), nil), nil).Register(g)

	return threadFixture{
		harness: harness{coach: svc, convos: convos, client: client, user: user, pool: pool},
		repo:    repo,
		router:  e,
	}
}

// seedThread stores n alternating turns and returns their ids, oldest first.
func (f threadFixture) seedThread(t *testing.T, n int) (uuid.UUID, []uuid.UUID) {
	t.Helper()

	ctx := context.Background()
	conversationID := newConversation(t, f.harness)
	if err := f.convos.SetTitle(ctx, conversationID, "A long thread"); err != nil {
		t.Fatal(err)
	}
	ids := make([]uuid.UUID, 0, n)
	for i := range n {
		var (
			m   conversations.Message
			err error
		)
		if i%2 == 0 {
			m, err = f.convos.AppendUserMessage(ctx, conversationID, fmt.Sprintf("question %d", i), nil)
		} else {
			m, err = f.convos.AppendModelMessage(ctx, conversationID, fmt.Sprintf("answer %d", i), nil, "m", "p", nil, conversations.Provenance{})
		}
		if err != nil {
			t.Fatalf("seed turn %d: %v", i, err)
		}
		ids = append(ids, m.ID)
	}
	return conversationID, ids
}
