package crews_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/crews"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
)

// The join link, as a browser takes it: signed out it stores the code and
// the first page inside joins; signed in it joins at once and opens the board.
func TestJoinLink(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	svc := service(pool)
	ana := person(t, pool, "ana@north.test", "Ana")
	leo := person(t, pool, "leo@north.test", "Leo")
	zoe := person(t, pool, "zoe@north.test", "Zoe")
	c, _ := svc.Create(ctx, ana.ID, "Duo")
	h := crews.NewHandler(svc, "https://kheprios.com", true)
	public := chi.NewRouter()
	h.PublicRoutes(public)

	rec := httptest.NewRecorder()
	public.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/i/c/"+c.Code, nil))
	var cookie *http.Cookie
	for _, k := range rec.Result().Cookies() {
		if k.Name == crews.JoinCookie {
			cookie = k
		}
	}
	if rec.Code != http.StatusOK || cookie == nil || cookie.Value != c.Code || !cookie.Secure {
		t.Fatalf("signed out: %d, cookie %+v", rec.Code, cookie)
	}
	req := httptest.NewRequest(http.MethodGet, "/app", nil)
	req.AddCookie(cookie)
	req = req.WithContext(auth.ContextWithUser(req.Context(), leo))
	h.JoinFromCookie(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(httptest.NewRecorder(), req)
	if b, err := svc.Board(ctx, c.ID, leo.ID); err != nil || len(b.Members) != 2 {
		t.Fatalf("leo did not join from the cookie: %v", err)
	}

	req = httptest.NewRequest(http.MethodGet, "/i/c/"+c.Code, nil)
	req = req.WithContext(auth.ContextWithUser(req.Context(), zoe))
	rec = httptest.NewRecorder()
	public.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/app/crews/"+c.ID.String() {
		t.Fatalf("signed in: %d → %q", rec.Code, rec.Header().Get("Location"))
	}
}
