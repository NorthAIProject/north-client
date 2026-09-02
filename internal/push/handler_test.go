package push_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/push"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

// newRouter mounts the handler behind a stand-in for RequireAuth that signs
// every request in as user. CSRF is not in this router: it is a middleware the
// whole app shares and is pinned by its own tests; what this file checks is
// what the handler does with a request that got through.
func newRouter(svc *push.Service, user users.User) http.Handler {
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(auth.ContextWithUser(req.Context(), user)))
		})
	})
	push.NewHandler(svc).Routes(r)
	return r
}

func do(h http.Handler, method, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/push/subscriptions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone) Test")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

const browserJSON = `{"endpoint":"https://push.example/browser","expirationTime":null,"keys":{"p256dh":"BNcRdreALRFXTkOOUHK1EtK2wtaz5Ry4YfYCA_0QTpQtUbVlUls0VJXg7A8u-Ts1XbjhazAkj7I99e8QcYP7DkM","auth":"tBHItJI5svbpez7KI4CCXg"}}`

func handlerSetup(t *testing.T) (*pgxpool.Pool, *push.Service, users.User) {
	t.Helper()
	pool := testdb.New(t)
	user := seedUser(t, pool, "push-handler@north.test")
	svc := newService(pool, &fakeSender{status: 201})
	return pool, svc, user
}

// The body is PushSubscription.toJSON() verbatim, expirationTime included,
// because that is what the page posts and a decoder that choked on a field the
// browser always sends would refuse every real subscription.
func TestSubscribeAcceptsWhatTheBrowserPosts(t *testing.T) {
	_, svc, user := handlerSetup(t)
	h := newRouter(svc, user)

	w := do(h, http.MethodPost, browserJSON)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body %q; want 204", w.Code, w.Body.String())
	}

	n, err := svc.Count(context.Background(), user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("count = %d, want 1", n)
	}
}

func TestSubscribeAnswersBadInputWithTheRightStatus(t *testing.T) {
	_, svc, user := handlerSetup(t)
	h := newRouter(svc, user)

	cases := map[string]struct {
		body string
		want int
	}{
		"not json":      {body: "<subscription/>", want: http.StatusBadRequest},
		"http endpoint": {body: strings.Replace(browserJSON, "https://", "http://", 1), want: http.StatusUnprocessableEntity},
		"missing keys":  {body: `{"endpoint":"https://push.example/x","keys":{}}`, want: http.StatusUnprocessableEntity},
		"oversized":     {body: `{"endpoint":"https://push.example/` + strings.Repeat("x", 9000) + `","keys":{"p256dh":"a","auth":"b"}}`, want: http.StatusRequestEntityTooLarge},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := do(h, http.MethodPost, tc.body)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %q)", w.Code, tc.want, w.Body.String())
			}
		})
	}

	if n, _ := svc.Count(context.Background(), user.ID); n != 0 {
		t.Fatalf("a refused request stored a subscription; count = %d", n)
	}
}

func TestUnsubscribeRemovesTheBrowser(t *testing.T) {
	_, svc, user := handlerSetup(t)
	h := newRouter(svc, user)

	if w := do(h, http.MethodPost, browserJSON); w.Code != http.StatusNoContent {
		t.Fatalf("subscribe: %d", w.Code)
	}
	w := do(h, http.MethodDelete, `{"endpoint":"https://push.example/browser"}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", w.Code)
	}
	if n, _ := svc.Count(context.Background(), user.ID); n != 0 {
		t.Fatalf("count = %d after unsubscribe, want 0", n)
	}

	if w := do(h, http.MethodDelete, `{}`); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unsubscribe with no endpoint: %d, want 422", w.Code)
	}
}

// A deployment without keys has no such endpoint. 404 rather than 503 so a
// probe learns nothing about whether the feature exists here.
func TestSubscribeIsNotFoundWhenPushIsOff(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool, "push-handler-off@north.test")
	svc := push.NewService(push.NewRepository(pool), nil, push.VAPID{}, nil)
	h := newRouter(svc, user)

	if w := do(h, http.MethodPost, browserJSON); w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}
