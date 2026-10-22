package achievements_test

import (
	"context"
	"testing"

	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

func TestSharingOnDefaults(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	a, err := users.NewService(users.NewRepository(pool)).Register(ctx, users.Registration{
		Email: "sharing-default@example.com", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: "Ada", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	on := func(category string) bool {
		t.Helper()
		var v bool
		if err := pool.QueryRow(ctx, `SELECT sharing_on($1, $2)`, a.ID, category).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	for c, want := range map[string]bool{"training": true, "streaks": true, "goals": true, "xp": false} {
		if got := on(c); got != want {
			t.Errorf("sharing_on(%s) = %v, want %v", c, got, want)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO achievement_sharing (user_id, category, shared) VALUES ($1, 'training', false)`, a.ID); err != nil {
		t.Fatal(err)
	}
	if on("training") {
		t.Error("stored false row must beat the default")
	}
}
