package lifts_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/lifts"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

// The iOS workout screen sends a set again when it never heard back. A retry
// after a lost response carries the same clientId and must not log the set
// twice; it answers with the set already logged.
func TestARetriedSetWithTheSameClientIDIsLoggedOnce(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	reg := users.NewService(users.NewRepository(pool))
	register := func(email string) users.User {
		t.Helper()
		u, err := reg.Register(ctx, users.Registration{
			Email: email, PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: "R", Timezone: "UTC",
		})
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	me := register("retries@example.com")
	other := register("retries-other@example.com")

	router := chi.NewRouter()
	lifts.NewAPI(lifts.NewService(lifts.NewRepository(pool), nil)).Routes(router)
	post := func(user users.User, body string) (int, lifts.LiftSetView) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/lifts/sets", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(auth.ContextWithUser(req.Context(), user))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		var view lifts.LiftSetView
		if rec.Code < 300 {
			if err := json.Unmarshal(rec.Body.Bytes(), &view); err != nil {
				t.Fatal(err)
			}
		}
		return rec.Code, view
	}
	count := func(user users.User) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM set_logs WHERE user_id = $1`, user.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	clientID := uuid.NewString()
	set := `{"exerciseName":"Back squat","setNumber":1,"weightKg":100,"reps":5,"clientId":"` + clientID + `"}`
	status, first := post(me, set)
	if status != http.StatusCreated {
		t.Fatalf("first upload: status %d", status)
	}
	status, again := post(me, set)
	if status != http.StatusCreated {
		t.Fatalf("retry: status %d", status)
	}
	if again.ID != first.ID {
		t.Errorf("the retry answered set %s, want the one already logged, %s", again.ID, first.ID)
	}
	if n := count(me); n != 1 {
		t.Fatalf("a retried set is %d rows, want 1", n)
	}

	// The id is the client's, so it is scoped to the account: another person's
	// phone that happened on the same one logs its own set.
	if status, theirs := post(other, set); status != http.StatusCreated || theirs.ID == first.ID {
		t.Fatalf("another account's set with the same clientId: status %d, id %s", status, theirs.ID)
	}

	// Without a clientId, every upload is a new set, as before.
	plain := `{"exerciseName":"Back squat","setNumber":2,"weightKg":100,"reps":5}`
	post(me, plain)
	post(me, plain)
	if n := count(me); n != 3 {
		t.Fatalf("two uploads without a clientId made %d rows in all, want 3", n)
	}
}
