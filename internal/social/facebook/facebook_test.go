package facebook_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/social/facebook"
)

const token = "EAAB-secret-token"

// fakeGraph is enough of the Graph API for one import: the code exchange,
// /me, and two pages of /me/friends.
func fakeGraph(t *testing.T) (*facebook.Client, *[]string) {
	t.Helper()
	var calls []string
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		calls = append(calls, r.URL.Path)
		if r.URL.Path != "/oauth/access_token" && q.Get("access_token") != token {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"type":"OAuthException","code":190}}`))
			return
		}
		var body any
		switch {
		case r.URL.Path == "/oauth/access_token":
			if q.Get("code") != "the-code" || q.Get("client_secret") != "app-secret" || q.Get("redirect_uri") != "https://kheprios.com/cb" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"type":"OAuthException","code":100}}`))
				return
			}
			body = map[string]any{"access_token": token, "token_type": "bearer"}
		case r.URL.Path == "/me":
			body = map[string]any{"id": "fb-me"}
		case r.URL.Path == "/me/friends" && q.Get("after") == "":
			next := srv.URL + "/me/friends?" + url.Values{"access_token": {token}, "after": {"p2"}}.Encode()
			body = map[string]any{"data": []map[string]string{{"id": "fb-ana"}, {"id": "fb-joao"}}, "paging": map[string]string{"next": next}}
		case r.URL.Path == "/me/friends":
			body = map[string]any{"data": []map[string]string{{"id": "fb-rita"}}, "paging": map[string]string{}}
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	c := facebook.New("app-id", "app-secret")
	c.GraphURL = srv.URL
	c.HTTP = srv.Client()
	return c, &calls
}

func TestImportReadsEveryFriendPage(t *testing.T) {
	t.Parallel()
	c, calls := fakeGraph(t)
	got, err := c.Import(context.Background(), "the-code", "https://kheprios.com/cb")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "fb-me" || strings.Join(got.FriendIDs, ",") != "fb-ana,fb-joao,fb-rita" {
		t.Fatalf("account = %+v", got)
	}
	if len(*calls) != 4 {
		t.Fatalf("calls = %v", *calls)
	}
}

func TestImportFailureNamesNoSecret(t *testing.T) {
	t.Parallel()
	c, _ := fakeGraph(t)
	_, err := c.Import(context.Background(), "a-wrong-code", "https://kheprios.com/cb")
	if err == nil {
		t.Fatal("a wrong code imported")
	}
	for _, secret := range []string{"a-wrong-code", "app-secret", token} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error %q leaks %q", err, secret)
		}
	}
}

func TestAuthCodeURL(t *testing.T) {
	t.Parallel()
	if facebook.New("", "secret") != nil || facebook.New("id", " ") != nil {
		t.Fatal("a half-configured client was built")
	}
	u, err := url.Parse(facebook.New("app-id", "app-secret").AuthCodeURL("st4te", "https://kheprios.com/cb"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Host != "www.facebook.com" || q.Get("client_id") != "app-id" || q.Get("state") != "st4te" ||
		q.Get("redirect_uri") != "https://kheprios.com/cb" || q.Get("scope") != facebook.Scopes {
		t.Fatalf("consent url = %s", u)
	}
	if strings.Contains(u.String(), "app-secret") {
		t.Fatal("the app secret is in the consent url")
	}
}

// A paging link that leaves the Graph API is not followed: the token would
// go with it.
func TestImportFollowsPagingOnlyBackToGraph(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body any
		switch r.URL.Path {
		case "/oauth/access_token":
			body = map[string]any{"access_token": token}
		case "/me":
			body = map[string]any{"id": "fb-me"}
		default:
			body = map[string]any{
				"data":   []map[string]string{{"id": "fb-ana"}},
				"paging": map[string]string{"next": "https://evil.test/me/friends?after=p2"},
			}
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	c := facebook.New("app-id", "app-secret")
	c.GraphURL, c.HTTP = srv.URL, srv.Client()
	got, err := c.Import(context.Background(), "code", "https://kheprios.com/cb")
	if err != nil || strings.Join(got.FriendIDs, ",") != "fb-ana" {
		t.Fatalf("import = %+v, %v", got, err)
	}
}
