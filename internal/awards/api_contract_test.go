package awards_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/awards"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	"github.com/NorthAIProject/north-client/internal/users"
)

type apiEnv struct {
	t    *testing.T
	db   *pgxpool.Pool
	svc  *awards.Service
	r    http.Handler
	user users.User
}

func newAPIEnv(t *testing.T, email string) *apiEnv {
	t.Helper()
	pool := testdb.New(t)
	svc := awards.NewService(pool)
	r := echox.New(echox.Config{})
	awards.NewAPI(svc).Register(r.Group(""))
	u, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email: email, PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: "Pat", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	return &apiEnv{t: t, db: pool, svc: svc, r: r, user: u}
}

func (e *apiEnv) do(method, path string, as users.User) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	e.r.ServeHTTP(rec, req.WithContext(auth.ContextWithUser(req.Context(), as)))
	return rec
}

func TestUnseenThenSeenOverHTTP(t *testing.T) {
	e := newAPIEnv(t, "unseen@north.test")
	other := newAPIEnv(t, "unseen-other@north.test")
	e.svc.Grant(context.Background(), e.user.ID, time.Now(), "workouts_1")

	unseen := func() int {
		rec := e.do(http.MethodGet, "/awards/unseen", e.user)
		var v struct{ Awards []map[string]any }
		if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return len(v.Awards)
	}
	if unseen() != 1 {
		t.Fatal("badge not listed as unseen")
	}
	if c := e.do(http.MethodPost, "/awards/workouts_1/seen", e.user).Code; c != http.StatusNoContent {
		t.Fatalf("seen = %d", c)
	}
	if unseen() != 0 {
		t.Fatal("still unseen after seen")
	}
	if c := e.do(http.MethodPost, "/awards/workouts_1/seen", e.user).Code; c != http.StatusNoContent {
		t.Fatalf("second seen = %d", c)
	}
	if c := e.do(http.MethodPost, "/awards/workouts_1/seen", other.user).Code; c != http.StatusNotFound {
		t.Fatalf("another user's seen = %d, want 404", c)
	}
	if c := e.do(http.MethodPost, "/awards/nope/seen", e.user).Code; c != http.StatusNotFound {
		t.Fatalf("unknown badge = %d, want 404", c)
	}
}

func TestAwardsListSplitsEarnedAndLocked(t *testing.T) {
	e := newAPIEnv(t, "list@north.test")
	e.svc.Grant(context.Background(), e.user.ID, time.Now(), "workouts_1")
	rec := e.do(http.MethodGet, "/awards", e.user)
	var v struct {
		Earned []struct{ ID string }
		Locked []struct {
			ID       string
			Progress *int
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Earned) != 1 || v.Earned[0].ID != "workouts_1" || len(v.Locked) != 30 {
		t.Fatalf("earned %d locked %d", len(v.Earned), len(v.Locked))
	}
	for _, l := range v.Locked {
		if l.Progress == nil {
			t.Fatalf("%s has no progress", l.ID)
		}
	}
}

func (e *apiEnv) pool() *pgxpool.Pool { return e.db }
