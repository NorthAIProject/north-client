package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/users"
)

// tokenResolver knows a fixed set of tokens, and counts how often it was asked:
// each ask is one session query in production.
type tokenResolver struct {
	mu       sync.Mutex
	resolved int
	accounts map[string]uuid.UUID
}

func (r *tokenResolver) Resolve(_ context.Context, token string) (auth.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resolved++
	id, ok := r.accounts[token]
	if !ok {
		return auth.Session{}, apperr.ErrUnauthenticated
	}
	return auth.Session{User: users.User{ID: id}}, nil
}

func limitedRouter(limits auth.RouteLimits, proxies middleware.TrustedProxies, tokens ...string) (*echo.Echo, *tokenResolver) {
	sessions := &tokenResolver{accounts: map[string]uuid.UUID{}}
	for _, token := range tokens {
		sessions.accounts[token] = uuid.New()
	}
	limiter := auth.NewRouteLimiter(limits, nil)

	e := echox.New(echox.Config{TrustedProxies: proxies})
	g := e.Group("", limiter.PerAddress(), auth.Bearer(sessions), limiter.PerAccount())
	g.GET("/thing", func(c *echo.Context) error { return c.NoContent(http.StatusNoContent) })
	return e, sessions
}

func call(e *echo.Echo, remote, token string, headers ...[2]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/thing", nil)
	req.RemoteAddr = remote
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for _, h := range headers {
		req.Header.Set(h[0], h[1])
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func wantRefused(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("a 429 with no Retry-After leaves the client guessing")
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("content type = %q, want the JSON every API failure uses", got)
	}
}

// One account past its bound is refused from any address, and nobody else is.
func TestAnAccountPastItsBoundIsRefusedAndOnlyThatAccount(t *testing.T) {
	t.Parallel()

	e, _ := limitedRouter(auth.RouteLimits{PerAddressPerMinute: 1000, PerAccountPerMinute: 2}, nil, "good-a", "good-b")

	for i := range 2 {
		if rec := call(e, "203.0.113.1:5000", "good-a"); rec.Code != http.StatusNoContent {
			t.Fatalf("request %d: status = %d, want 204", i+1, rec.Code)
		}
	}
	wantRefused(t, call(e, "198.51.100.9:5000", "good-a"))

	if rec := call(e, "203.0.113.1:5000", "good-b"); rec.Code != http.StatusNoContent {
		t.Errorf("another account at the same address: status = %d, want 204", rec.Code)
	}
}

// The point of the address bucket: a loop of bad tokens stops reaching the
// database. Without it every request costs one session query.
func TestAnAddressPastItsBoundNeverReachesTheSessionLookup(t *testing.T) {
	t.Parallel()

	e, sessions := limitedRouter(auth.RouteLimits{PerAddressPerMinute: 3, PerAccountPerMinute: 1000}, nil, "good-a")

	for range 3 {
		if rec := call(e, "203.0.113.1:5000", "garbage"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	}
	wantRefused(t, call(e, "203.0.113.1:5000", "garbage"))

	sessions.mu.Lock()
	resolved := sessions.resolved
	sessions.mu.Unlock()
	if resolved != 3 {
		t.Errorf("%d session lookups, want 3: the refused request reached the database", resolved)
	}

	if rec := call(e, "203.0.113.2:5000", "good-a"); rec.Code != http.StatusNoContent {
		t.Errorf("a different address: status = %d, want 204", rec.Code)
	}
}

// A bad token must not spend a real account's allowance.
func TestABadTokenSpendsNoAccountAllowance(t *testing.T) {
	t.Parallel()

	e, _ := limitedRouter(auth.RouteLimits{PerAddressPerMinute: 1000, PerAccountPerMinute: 1}, nil, "good-a")

	for range 5 {
		call(e, "203.0.113.1:5000", "garbage")
	}
	if rec := call(e, "203.0.113.1:5000", "good-a"); rec.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", rec.Code)
	}
}

// The same address logic the sign-in throttle uses: behind the ingress and
// Cloudflare's edge, callers are told apart by CF-Connecting-IP, and a request
// from anywhere else cannot pick its own bucket with that header.
func TestAddressesAreTheOnesTheEdgeVouchesFor(t *testing.T) {
	t.Parallel()

	const (
		ingress = "10.42.0.7:5000"
		edge    = "172.68.1.1" // In Cloudflare's published ranges.
		offEdge = "198.51.100.9"
	)
	proxies := middleware.TrustedProxies{netip.MustParsePrefix("10.42.0.0/16")}
	limits := auth.RouteLimits{PerAddressPerMinute: 1, PerAccountPerMinute: 1000}

	from := func(hop, caller string) [][2]string {
		return [][2]string{{"X-Forwarded-For", hop}, {"CF-Connecting-IP", caller}}
	}

	t.Run("two callers behind one edge get a bucket each", func(t *testing.T) {
		t.Parallel()
		e, _ := limitedRouter(limits, proxies)
		for _, caller := range []string{"203.0.113.1", "203.0.113.2"} {
			if rec := call(e, ingress, "", from(edge, caller)...); rec.Code != http.StatusUnauthorized {
				t.Errorf("%s: status = %d, want 401: strangers behind one edge shared a bucket", caller, rec.Code)
			}
		}
		wantRefused(t, call(e, ingress, "", from(edge, "203.0.113.1")...))
	})

	t.Run("the header buys nothing from off the edge", func(t *testing.T) {
		t.Parallel()
		e, _ := limitedRouter(limits, proxies)
		if rec := call(e, ingress, "", from(offEdge, "203.0.113.1")...); rec.Code != http.StatusUnauthorized {
			t.Fatalf("first request: status = %d, want 401", rec.Code)
		}
		wantRefused(t, call(e, ingress, "", from(offEdge, "203.0.113.2")...))
	})

	t.Run("no trusted proxies means the peer, whatever it claims", func(t *testing.T) {
		t.Parallel()
		e, _ := limitedRouter(limits, nil)
		if rec := call(e, "203.0.113.1:5000", "", [2]string{"X-Forwarded-For", "198.51.100.1"}); rec.Code != http.StatusUnauthorized {
			t.Fatalf("first request: status = %d, want 401", rec.Code)
		}
		wantRefused(t, call(e, "203.0.113.1:5000", "", [2]string{"X-Forwarded-For", "198.51.100.2"}))
	})
}

// What real clients do, at the real limits. Twenty heavy pages of 15 calls and
// ten health batches, all at once from one phone, is well past anything a thumb
// produces in a minute.
func TestABurstFromOnePhoneIsNotRefusedAtTheDefaultLimits(t *testing.T) {
	t.Parallel()

	e, _ := limitedRouter(auth.RouteLimits{}, nil, "good-a")

	const requests = 20*15 + 10
	refused := make(chan int, requests)
	var wg sync.WaitGroup
	for range requests {
		wg.Go(func() {
			if rec := call(e, "203.0.113.1:5000", "good-a"); rec.Code != http.StatusNoContent {
				refused <- rec.Code
			}
		})
	}
	wg.Wait()
	close(refused)
	for code := range refused {
		t.Fatalf("a real burst was refused with %d", code)
	}
}

// Seven people behind one address, an office or a carrier's NAT, each doing that
// same heavy burst: within both bounds.
func TestPeopleSharingAnAddressAreNotRefusedAtTheDefaultLimits(t *testing.T) {
	t.Parallel()

	tokens := []string{"p1", "p2", "p3", "p4", "p5", "p6", "p7"}
	e, _ := limitedRouter(auth.RouteLimits{}, nil, tokens...)

	for _, token := range tokens {
		for range 300 {
			if rec := call(e, "203.0.113.50:5000", token); rec.Code != http.StatusNoContent {
				t.Fatalf("%s behind a shared address: status = %d", token, rec.Code)
			}
		}
	}
}

// A misconfigured limit falls back rather than refusing everything.
func TestAnEmptyConfigurationUsesTheDefaults(t *testing.T) {
	t.Parallel()

	e, _ := limitedRouter(auth.RouteLimits{PerAddressPerMinute: 0, PerAccountPerMinute: -1}, nil, "good-a")
	for i := range auth.DefaultBearerRequestsPerAccount {
		if rec := call(e, "203.0.113.1:5000", "good-a"); rec.Code != http.StatusNoContent {
			t.Fatalf("request %d: status = %d, want 204", i+1, rec.Code)
		}
	}
}
