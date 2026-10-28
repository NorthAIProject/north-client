package achievements_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/achievements"
	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
)

// A client from before awards saves the three switches it knows. That must
// not move the one it has never heard of, whichever way it was set.
func TestPutSharingWithoutAwardsKeepsIt(t *testing.T) {
	ctx := context.Background()
	pool := testdb.New(t)
	svc := achievements.NewService(pool)
	u := person(t, pool, "put-awards@north.test", "Pru")

	e := echo.New()
	achievements.NewAPI(svc).Register(e.Group(""))
	put := func(body string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/social/sharing", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		e.ServeHTTP(rec, req.WithContext(auth.ContextWithUser(req.Context(), u)))
		if rec.Code != http.StatusOK {
			t.Fatalf("PUT %s = %d %s", body, rec.Code, rec.Body)
		}
	}
	awards := func() bool {
		t.Helper()
		s, err := svc.Sharing(ctx, u.ID)
		if err != nil {
			t.Fatal(err)
		}
		return s.Awards
	}

	put(`{"training":true,"streaks":true,"goals":true,"awards":false}`)
	if awards() {
		t.Fatal("awards:false was not stored")
	}
	put(`{"training":true,"streaks":true,"goals":true}`)
	if awards() {
		t.Fatal("a PUT without awards switched a stored false back on")
	}
	put(`{"training":true,"streaks":true,"goals":true,"awards":true}`)
	put(`{"training":false,"streaks":false,"goals":false}`)
	if !awards() {
		t.Fatal("a PUT without awards switched a stored true off")
	}
}
