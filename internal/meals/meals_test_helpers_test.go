package meals_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/users"
)

// newUser creates a real account against pool, so ownership checks in these
// tests exercise a genuine row rather than a bare uuid.
func newUser(t *testing.T, pool *pgxpool.Pool, email string) users.User {
	t.Helper()

	userSvc := users.NewService(users.NewRepository(pool))
	user, err := userSvc.Register(context.Background(), users.Registration{
		Email:        email,
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Test User",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	return user
}
