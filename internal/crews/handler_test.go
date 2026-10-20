package crews_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/crews"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
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

func webGet(t *testing.T, base, path string, as users.User) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, base+path, nil)
	req.Header.Set("X-Test-User", as.ID.String())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func webPost(t *testing.T, base, path string, as users.User, form url.Values) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, base+path, strings.NewReader(form.Encode()))
	req.Header.Set("X-Test-User", as.ID.String())
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func TestMessagesFragmentAfterCursor(t *testing.T) {
	r := newRoom(t)
	s := newLiveServer(t, r)
	first := r.send(t, r.ana, "first one")
	r.send(t, r.leo, "second one")
	last := r.send(t, r.ana, "third one")

	status, html := webGet(t, s.web.URL, fmt.Sprintf("/crews/%s/c/%s/messages?after=%d", r.crew.ID, r.general, first.Seq), r.owner)
	if status != http.StatusOK {
		t.Fatalf("status %d", status)
	}
	if strings.Contains(html, "first one") || !strings.Contains(html, "second one") || !strings.Contains(html, "third one") {
		t.Fatalf("fragment after %d:\n%s", first.Seq, html)
	}
	if !strings.Contains(html, `hx-swap-oob="true"`) || !strings.Contains(html, fmt.Sprintf(`value="%d"`, last.Seq)) {
		t.Fatalf("no out-of-band cursor at %d:\n%s", last.Seq, html)
	}
}

func TestComposerTermsRequired(t *testing.T) {
	r := newRoom(t)
	s := newLiveServer(t, r)
	zed := handled(t, r.pool, "zed@north.test", "Zed", "zed")
	if _, err := r.svc.Join(context.Background(), zed.ID, r.crew.Code); err != nil {
		t.Fatal(err)
	}
	status, html := webPost(t, s.web.URL, "/crews/"+r.crew.ID.String()+"/c/"+r.general.String()+"/messages", zed, url.Values{"body": {"hi"}})
	// 422, not 403: the layout's htmx config discards every 4xx except 422
	// and 429, so a 403 refusal would never reach the slot under the box.
	if status != http.StatusUnprocessableEntity || !strings.Contains(html, "data-terms-required") {
		t.Fatalf("status %d:\n%s", status, html)
	}
	status, _ = webPost(t, s.web.URL, "/crews/"+r.crew.ID.String()+"/c/"+r.general.String()+"/messages", r.ana, url.Values{"body": {"hi"}})
	if status != http.StatusNoContent {
		t.Fatalf("Ana, who accepted, got %d", status)
	}
}

func TestOldCrewURLStillWorks(t *testing.T) {
	r := newRoom(t)
	s := newLiveServer(t, r)
	status, html := webGet(t, s.web.URL, "/crews/"+r.crew.ID.String(), r.ana)
	if status != http.StatusOK || !strings.Contains(html, "hx-sse:connect") || !strings.Contains(html, "general") {
		t.Fatalf("status %d:\n%s", status, html)
	}
	status, _ = webGet(t, s.web.URL, "/crews/"+r.crew.ID.String()+"/challenge", r.ana)
	if status != http.StatusOK {
		t.Fatalf("challenge tab: %d", status)
	}
}
