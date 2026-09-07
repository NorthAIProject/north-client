package mcpauth_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/connections"
	"github.com/NorthAIProject/north-client/internal/mcpauth"
	"github.com/NorthAIProject/north-client/internal/mcpserver"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

const baseURL = "https://north.test"

const testVerifier = "sN1kR3vLp8qWzYx2AcBdEfGhIjKlMnOpQrStUvWxYz0"

func testChallenge() string {
	sum := sha256.Sum256([]byte(testVerifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// grantSpy wraps the real connections service so a test can see what was
// revoked. The grant half is real, because "the access token authenticates
// afterwards" is most of what these tests are checking.
type grantSpy struct {
	*connections.Service

	revoked []uuid.UUID
}

func (g *grantSpy) Revoke(ctx context.Context, id, userID uuid.UUID) error {
	g.revoked = append(g.revoked, id)
	return g.Service.Revoke(ctx, id, userID)
}

type harness struct {
	svc    *mcpauth.Service
	conns  *connections.Service
	grants *grantSpy
	user   users.User
}

func newHarness(t *testing.T) harness {
	t.Helper()

	pool := testdb.New(t)
	userSvc := users.NewService(users.NewRepository(pool))

	user, err := userSvc.Register(context.Background(), users.Registration{
		Email:        "fernando@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Fernando Correia",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("register a user: %v", err)
	}

	conns := connections.NewService(connections.NewRepository(pool), userSvc, baseURL)
	grants := &grantSpy{Service: conns}

	return harness{
		svc:    mcpauth.NewService(mcpauth.NewRepository(pool), grants, baseURL),
		conns:  conns,
		grants: grants,
		user:   user,
	}
}

func (h harness) client(t *testing.T, redirectURIs ...string) mcpauth.Client {
	t.Helper()
	if len(redirectURIs) == 0 {
		redirectURIs = []string{"https://claude.ai/api/mcp/auth_callback"}
	}

	client, err := h.svc.RegisterClient(context.Background(), mcpauth.Registration{
		ClientName:   "Claude Code",
		RedirectURIs: redirectURIs,
	})
	if err != nil {
		t.Fatalf("register a client: %v", err)
	}
	return client
}

// authorize runs BeginAuthorization with sensible defaults and returns the
// parked request plus its browser cookie.
func (h harness) authorize(t *testing.T, client mcpauth.Client, scope string) (mcpauth.Request, string) {
	t.Helper()

	req, verifier, err := h.svc.BeginAuthorization(context.Background(), mcpauth.AuthorizeParams{
		ClientID:            client.ID,
		RedirectURI:         client.RedirectURIs[0],
		ResponseType:        "code",
		State:               "opaque-state",
		Scope:               scope,
		CodeChallenge:       testChallenge(),
		CodeChallengeMethod: mcpauth.ChallengeMethodS256,
	}, false)
	if err != nil {
		t.Fatalf("begin authorization: %v", err)
	}
	return req, verifier
}

// codeFrom pulls the authorization code out of an approval redirect.
func codeFrom(t *testing.T, redirect string) string {
	t.Helper()
	parsed, err := url.Parse(redirect)
	if err != nil {
		t.Fatalf("parse the approval redirect: %v", err)
	}
	code := parsed.Query().Get("code")
	if code == "" {
		t.Fatalf("no code in %s", redirect)
	}
	return code
}

// The whole flow, once: register, authorize, approve, exchange, and the access
// token works against the same authenticator a pasted token uses.
func TestTheHappyPathIssuesAWorkingToken(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	client := h.client(t)
	req, verifier := h.authorize(t, client, mcpserver.ScopeReadWrite)

	redirect, err := h.svc.Approve(ctx, req.ID, verifier, h.user.ID, "")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}

	parsed, err := url.Parse(redirect)
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	if got := parsed.Query().Get("state"); got != "opaque-state" {
		t.Errorf("state is %q; a client matches its request by it", got)
	}

	tokens, err := h.svc.ExchangeCode(ctx, mcpauth.CodeExchange{
		Code:         codeFrom(t, redirect),
		ClientID:     client.ID,
		RedirectURI:  client.RedirectURIs[0],
		CodeVerifier: testVerifier,
	})
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}

	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		t.Fatal("the exchange returned an empty token")
	}
	if tokens.ExpiresIn != int(mcpauth.AccessTokenTTL.Seconds()) {
		t.Errorf("expires_in is %d, want %d", tokens.ExpiresIn, int(mcpauth.AccessTokenTTL.Seconds()))
	}

	gotUser, scope, err := h.conns.AuthenticateScoped(ctx, tokens.AccessToken)
	if err != nil {
		t.Fatalf("the issued access token does not authenticate: %v", err)
	}
	if gotUser.ID != h.user.ID {
		t.Errorf("the token authenticates as %s, want %s", gotUser.ID, h.user.ID)
	}
	if scope != mcpserver.ScopeReadWrite {
		t.Errorf("scope is %q, want %q", scope, mcpserver.ScopeReadWrite)
	}
}

// A code is single use, and presenting it a second time revokes everything it
// produced — RFC 6749 section 10.5. Whoever holds the second copy may hold the
// first, so the grant is not trustworthy any more.
func TestAReplayedCodeRevokesTheGrantItCreated(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	client := h.client(t)
	req, verifier := h.authorize(t, client, "")

	redirect, err := h.svc.Approve(ctx, req.ID, verifier, h.user.ID, "")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	code := codeFrom(t, redirect)

	exchange := mcpauth.CodeExchange{
		Code:         code,
		ClientID:     client.ID,
		RedirectURI:  client.RedirectURIs[0],
		CodeVerifier: testVerifier,
	}

	tokens, err := h.svc.ExchangeCode(ctx, exchange)
	if err != nil {
		t.Fatalf("first exchange: %v", err)
	}

	if _, replayErr := h.svc.ExchangeCode(ctx, exchange); replayErr == nil {
		t.Fatal("the code was accepted twice")
	}

	if len(h.grants.revoked) != 1 {
		t.Fatalf("a replay revoked %d grants, want 1", len(h.grants.revoked))
	}

	// And the access token the first exchange handed out is dead.
	if _, _, authErr := h.conns.AuthenticateScoped(ctx, tokens.AccessToken); !apperr.Is(authErr, apperr.ErrUnauthenticated) {
		t.Errorf("the access token still works after a replay revoked the grant: %v", authErr)
	}

	// So is its refresh token.
	if _, refreshErr := h.svc.Refresh(ctx, mcpauth.RefreshExchange{
		RefreshToken: tokens.RefreshToken,
		ClientID:     client.ID,
	}); refreshErr == nil {
		t.Error("the refresh token still works after a replay revoked the grant")
	}
}

// A refresh token is single use and rotates. Reuse means the credential has
// escaped, so every refresh token the grant holds is retired.
func TestARefreshTokenIsSingleUseAndReuseKillsTheRest(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	client := h.client(t)
	req, verifier := h.authorize(t, client, "")
	redirect, err := h.svc.Approve(ctx, req.ID, verifier, h.user.ID, "")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	first, err := h.svc.ExchangeCode(ctx, mcpauth.CodeExchange{
		Code:         codeFrom(t, redirect),
		ClientID:     client.ID,
		RedirectURI:  client.RedirectURIs[0],
		CodeVerifier: testVerifier,
	})
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}

	second, err := h.svc.Refresh(ctx, mcpauth.RefreshExchange{
		RefreshToken: first.RefreshToken,
		ClientID:     client.ID,
	})
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if second.RefreshToken == first.RefreshToken {
		t.Fatal("refreshing returned the same refresh token; it must rotate")
	}
	if _, _, authErr := h.conns.AuthenticateScoped(ctx, second.AccessToken); authErr != nil {
		t.Fatalf("the refreshed access token does not authenticate: %v", authErr)
	}
	if _, _, staleErr := h.conns.AuthenticateScoped(ctx, first.AccessToken); staleErr == nil {
		t.Error("the superseded access token still authenticates")
	}

	// Reusing the spent one retires the live one too.
	if _, reuseErr := h.svc.Refresh(ctx, mcpauth.RefreshExchange{
		RefreshToken: first.RefreshToken,
		ClientID:     client.ID,
	}); reuseErr == nil {
		t.Fatal("a spent refresh token was accepted")
	}
	if _, afterErr := h.svc.Refresh(ctx, mcpauth.RefreshExchange{
		RefreshToken: second.RefreshToken,
		ClientID:     client.ID,
	}); afterErr == nil {
		t.Error("the live refresh token survived a detected reuse of its predecessor")
	}
}

// The token endpoint's refusals, each of which is a specific attack.
func TestTheTokenEndpointRefusals(t *testing.T) {
	newCode := func(t *testing.T, h harness, client mcpauth.Client) string {
		t.Helper()
		req, verifier := h.authorize(t, client, "")
		redirect, err := h.svc.Approve(context.Background(), req.ID, verifier, h.user.ID, "")
		if err != nil {
			t.Fatalf("approve: %v", err)
		}
		return codeFrom(t, redirect)
	}

	t.Run("a code issued to one client cannot be redeemed by another", func(t *testing.T) {
		h := newHarness(t)
		mine := h.client(t)
		theirs, err := h.svc.RegisterClient(context.Background(), mcpauth.Registration{
			ClientName:   "Someone Else",
			RedirectURIs: []string{"https://elsewhere.test/cb"},
		})
		if err != nil {
			t.Fatalf("register the second client: %v", err)
		}

		if _, err := h.svc.ExchangeCode(context.Background(), mcpauth.CodeExchange{
			Code:         newCode(t, h, mine),
			ClientID:     theirs.ID,
			RedirectURI:  mine.RedirectURIs[0],
			CodeVerifier: testVerifier,
		}); err == nil {
			t.Error("another client redeemed the code")
		}
	})

	t.Run("the redirect_uri must match the authorization request", func(t *testing.T) {
		h := newHarness(t)
		client := h.client(t,
			"https://claude.ai/api/mcp/auth_callback",
			"https://claude.ai/api/mcp/other")

		if _, err := h.svc.ExchangeCode(context.Background(), mcpauth.CodeExchange{
			Code:         newCode(t, h, client),
			ClientID:     client.ID,
			RedirectURI:  client.RedirectURIs[1],
			CodeVerifier: testVerifier,
		}); err == nil {
			t.Error("a different registered redirect_uri was accepted at the token endpoint")
		}
	})

	t.Run("the code_verifier must match the challenge", func(t *testing.T) {
		h := newHarness(t)
		client := h.client(t)

		if _, err := h.svc.ExchangeCode(context.Background(), mcpauth.CodeExchange{
			Code:         newCode(t, h, client),
			ClientID:     client.ID,
			RedirectURI:  client.RedirectURIs[0],
			CodeVerifier: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}); err == nil {
			t.Error("a wrong code_verifier was accepted")
		}
	})

	t.Run("an unknown code is refused", func(t *testing.T) {
		h := newHarness(t)
		client := h.client(t)

		if _, err := h.svc.ExchangeCode(context.Background(), mcpauth.CodeExchange{
			Code:         "not-a-code-that-was-ever-issued",
			ClientID:     client.ID,
			RedirectURI:  client.RedirectURIs[0],
			CodeVerifier: testVerifier,
		}); err == nil {
			t.Error("an unknown code was accepted")
		}
	})

	t.Run("a resource other than this deployment is refused", func(t *testing.T) {
		h := newHarness(t)
		client := h.client(t)

		if _, err := h.svc.ExchangeCode(context.Background(), mcpauth.CodeExchange{
			Code:         newCode(t, h, client),
			ClientID:     client.ID,
			RedirectURI:  client.RedirectURIs[0],
			CodeVerifier: testVerifier,
			Resource:     "https://someone-else.test/mcp",
		}); err == nil {
			t.Error("a token was issued for another resource")
		}
	})
}

// An expired code is refused. Sixty seconds is the browser redirect plus the
// client's token request, and nothing more.
func TestAnExpiredCodeIsRefused(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	client := h.client(t)
	req, verifier := h.authorize(t, client, "")

	// Minted against a clock far enough in the past that the code is already
	// dead by the time it is exchanged, then the real clock is restored so the
	// exchange sees the expiry rather than a shifted now.
	past := time.Now().Add(-2 * mcpauth.CodeTTL)
	redirect, err := h.svc.WithClock(func() time.Time { return past }).
		Approve(ctx, req.ID, verifier, h.user.ID, "")
	h.svc.WithClock(time.Now)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}

	if _, err := h.svc.ExchangeCode(ctx, mcpauth.CodeExchange{
		Code:         codeFrom(t, redirect),
		ClientID:     client.ID,
		RedirectURI:  client.RedirectURIs[0],
		CodeVerifier: testVerifier,
	}); err == nil {
		t.Error("an expired code was accepted")
	}
}

// The browser cookie is what binds a parked request to the browser that
// started it. Without it, a guessed request id would let somebody have a
// victim approve a request they started.
func TestAParkedRequestIsBoundToItsBrowser(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	client := h.client(t)
	req, verifier := h.authorize(t, client, "")

	for name, wrong := range map[string]string{
		"empty":     "",
		"different": "a-different-cookie-value-entirely",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := h.svc.LoadRequest(ctx, req.ID, wrong); err == nil {
				t.Error("the request loaded without its cookie")
			}
			if _, err := h.svc.Approve(ctx, req.ID, wrong, h.user.ID, ""); err == nil {
				t.Error("the request was approved without its cookie")
			}
		})
	}

	// And the right one still works, so the test is not passing because
	// everything is broken.
	if _, err := h.svc.LoadRequest(ctx, req.ID, verifier); err != nil {
		t.Errorf("the request did not load with its own cookie: %v", err)
	}
}

// Approve is single use. Two posts from a double-clicked button must not mint
// two codes for one consent.
func TestApproveCannotBeReplayed(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	client := h.client(t)
	req, verifier := h.authorize(t, client, "")

	if _, err := h.svc.Approve(ctx, req.ID, verifier, h.user.ID, ""); err != nil {
		t.Fatalf("first approve: %v", err)
	}
	if _, err := h.svc.Approve(ctx, req.ID, verifier, h.user.ID, ""); err == nil {
		t.Error("the same consent was approved twice")
	}
}

// The consent screen's read-only toggle may narrow a request, never widen it.
func TestConsentCanNarrowTheScopeButNotWidenIt(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	client := h.client(t)

	t.Run("read-write narrowed to read", func(t *testing.T) {
		req, verifier := h.authorize(t, client, mcpserver.ScopeReadWrite)
		redirect, err := h.svc.Approve(ctx, req.ID, verifier, h.user.ID, mcpserver.ScopeRead)
		if err != nil {
			t.Fatalf("approve: %v", err)
		}
		tokens, err := h.svc.ExchangeCode(ctx, mcpauth.CodeExchange{
			Code:         codeFrom(t, redirect),
			ClientID:     client.ID,
			RedirectURI:  client.RedirectURIs[0],
			CodeVerifier: testVerifier,
		})
		if err != nil {
			t.Fatalf("exchange: %v", err)
		}
		if _, scope, _ := h.conns.AuthenticateScoped(ctx, tokens.AccessToken); scope != mcpserver.ScopeRead {
			t.Errorf("granted scope is %q, want %q", scope, mcpserver.ScopeRead)
		}
	})

	t.Run("read cannot be widened to read-write", func(t *testing.T) {
		req, verifier := h.authorize(t, client, mcpserver.ScopeRead)
		if _, err := h.svc.Approve(ctx, req.ID, verifier, h.user.ID, mcpserver.ScopeReadWrite); err == nil {
			t.Error("a consent granted more than was requested")
		}
	})
}

// An unregistered client or callback must render a page, never redirect: the
// destination is unvalidated at that point, which is the classic
// open-redirect-via-OAuth bug.
func TestAnUntrustedDestinationIsNeverRedirectedTo(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	client := h.client(t)

	cases := map[string]mcpauth.AuthorizeParams{
		"an unknown client": {
			ClientID:            "mcpc_nonexistent",
			RedirectURI:         client.RedirectURIs[0],
			ResponseType:        "code",
			CodeChallenge:       testChallenge(),
			CodeChallengeMethod: mcpauth.ChallengeMethodS256,
		},
		"an unregistered callback": {
			ClientID:            client.ID,
			RedirectURI:         "https://evil.example/cb",
			ResponseType:        "code",
			CodeChallenge:       testChallenge(),
			CodeChallengeMethod: mcpauth.ChallengeMethodS256,
		},
		"no client at all": {
			RedirectURI:         client.RedirectURIs[0],
			ResponseType:        "code",
			CodeChallenge:       testChallenge(),
			CodeChallengeMethod: mcpauth.ChallengeMethodS256,
		},
	}

	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := h.svc.BeginAuthorization(ctx, params, false)
			if err == nil {
				t.Fatal("the request was accepted")
			}
			var fatal mcpauth.FatalAuthorizeError
			if !apperr.As(err, &fatal) {
				t.Errorf("error is %T (%v); it must be fatal so nothing redirects", err, err)
			}
		})
	}
}

// Once the callback is validated, a failure is reported to it — that is what
// lets a client show a useful message rather than hanging.
func TestAValidatedCallbackReceivesTheError(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	client := h.client(t)

	base := mcpauth.AuthorizeParams{
		ClientID:            client.ID,
		RedirectURI:         client.RedirectURIs[0],
		ResponseType:        "code",
		State:               "opaque-state",
		CodeChallenge:       testChallenge(),
		CodeChallengeMethod: mcpauth.ChallengeMethodS256,
	}

	cases := map[string]func(p *mcpauth.AuthorizeParams){
		"an unsupported response type": func(p *mcpauth.AuthorizeParams) { p.ResponseType = "token" },
		"a missing challenge":          func(p *mcpauth.AuthorizeParams) { p.CodeChallenge = "" },
		"the plain method":             func(p *mcpauth.AuthorizeParams) { p.CodeChallengeMethod = "plain" },
		"an unknown scope":             func(p *mcpauth.AuthorizeParams) { p.Scope = "north:admin" },
		"another resource":             func(p *mcpauth.AuthorizeParams) { p.Resource = "https://elsewhere.test/mcp" },
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			params := base
			mutate(&params)

			_, _, err := h.svc.BeginAuthorization(ctx, params, false)
			var redirectable mcpauth.RedirectableError
			if !apperr.As(err, &redirectable) {
				t.Fatalf("error is %T (%v); it must be redirectable", err, err)
			}
			if redirectable.RedirectURI != client.RedirectURIs[0] {
				t.Errorf("error points at %q, want the validated callback", redirectable.RedirectURI)
			}

			parsed, parseErr := url.Parse(redirectable.URL())
			if parseErr != nil {
				t.Fatalf("parse the error redirect: %v", parseErr)
			}
			if parsed.Query().Get("error") == "" {
				t.Error("the error redirect carries no error code")
			}
			if parsed.Query().Get("state") != "opaque-state" {
				t.Error("the error redirect dropped the state")
			}
		})
	}
}

// Registration is open, and every bound on it is worth a test because the
// endpoint is a public write.
func TestRegistrationBounds(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	rejected := map[string]mcpauth.Registration{
		"no name": {
			RedirectURIs: []string{"https://claude.ai/cb"},
		},
		"no redirect uri": {
			ClientName: "Claude Code",
		},
		"too many redirect uris": {
			ClientName: "Claude Code",
			RedirectURIs: []string{
				"https://a.test/cb", "https://b.test/cb", "https://c.test/cb",
				"https://d.test/cb", "https://e.test/cb", "https://f.test/cb",
			},
		},
		"an unsafe redirect uri": {
			ClientName:   "Claude Code",
			RedirectURIs: []string{"javascript:alert(1)"},
		},
		"a client that wants a secret": {
			ClientName:              "Claude Code",
			RedirectURIs:            []string{"https://claude.ai/cb"},
			TokenEndpointAuthMethod: "client_secret_post",
		},
		"an unsupported grant": {
			ClientName:   "Claude Code",
			RedirectURIs: []string{"https://claude.ai/cb"},
			GrantTypes:   []string{"password"},
		},
	}

	for name, reg := range rejected {
		t.Run("rejects "+name, func(t *testing.T) {
			if _, err := h.svc.RegisterClient(ctx, reg); err == nil {
				t.Error("the registration was accepted")
			}
		})
	}
}

// An identical registration returns the existing row, so a web client with a
// stable callback does not create one per attempt. A different one does not
// dedupe, which is what the sweep exists for.
func TestAnIdenticalRegistrationIsReused(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	reg := mcpauth.Registration{
		ClientName:   "Claude",
		RedirectURIs: []string{"https://claude.ai/api/mcp/auth_callback"},
	}

	first, err := h.svc.RegisterClient(ctx, reg)
	if err != nil {
		t.Fatalf("first registration: %v", err)
	}
	again, err := h.svc.RegisterClient(ctx, reg)
	if err != nil {
		t.Fatalf("second registration: %v", err)
	}
	if again.ID != first.ID {
		t.Errorf("an identical registration created a new client (%s, was %s)", again.ID, first.ID)
	}

	// The URI order is not part of the identity.
	reordered := mcpauth.Registration{
		ClientName: "Claude",
		RedirectURIs: []string{
			"https://claude.ai/api/mcp/auth_callback",
			"http://localhost:8123/cb",
		},
	}
	multi, err := h.svc.RegisterClient(ctx, reordered)
	if err != nil {
		t.Fatalf("register with two uris: %v", err)
	}
	swapped := mcpauth.Registration{
		ClientName: "Claude",
		RedirectURIs: []string{
			"http://localhost:8123/cb",
			"https://claude.ai/api/mcp/auth_callback",
		},
	}
	same, err := h.svc.RegisterClient(ctx, swapped)
	if err != nil {
		t.Fatalf("register with the uris swapped: %v", err)
	}
	if same.ID != multi.ID {
		t.Error("reordering the redirect uris created a second registration")
	}

	// A genuinely different name is a different client.
	other, err := h.svc.RegisterClient(ctx, mcpauth.Registration{
		ClientName:   "Codex",
		RedirectURIs: reg.RedirectURIs,
	})
	if err != nil {
		t.Fatalf("register a different client: %v", err)
	}
	if other.ID == first.ID {
		t.Error("a different client name reused an existing registration")
	}
}

// The consent screen shows the redirect host verbatim, because the client's
// name is chosen by whoever registered it and the host is not.
func TestTheRequestReportsWhereTheGrantIsGoing(t *testing.T) {
	h := newHarness(t)

	loopback := h.client(t, "http://127.0.0.1:41000/cb")
	req, _ := h.authorize(t, loopback, "")
	if got := req.RedirectHost(); got != "this computer" {
		t.Errorf("a loopback callback reads as %q, want %q", got, "this computer")
	}

	web, err := h.svc.RegisterClient(context.Background(), mcpauth.Registration{
		ClientName:   "Claude",
		RedirectURIs: []string{"https://claude.ai/api/mcp/auth_callback"},
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	webReq, _ := h.authorize(t, web, "")
	if got := webReq.RedirectHost(); got != "claude.ai" {
		t.Errorf("a web callback reads as %q, want claude.ai", got)
	}
}

// RFC 7009 lets a client hand back whichever token it holds. Either kills the
// whole grant: they are two halves of one connection, and a "disconnect" that
// left the other alive would not have disconnected.
func TestRevokingEitherTokenKillsTheGrant(t *testing.T) {
	issue := func(t *testing.T) (harness, mcpauth.Tokens) {
		t.Helper()
		h := newHarness(t)
		client := h.client(t)
		req, verifier := h.authorize(t, client, "")
		redirect, err := h.svc.Approve(context.Background(), req.ID, verifier, h.user.ID, "")
		if err != nil {
			t.Fatalf("approve: %v", err)
		}
		tokens, err := h.svc.ExchangeCode(context.Background(), mcpauth.CodeExchange{
			Code:         codeFrom(t, redirect),
			ClientID:     client.ID,
			RedirectURI:  client.RedirectURIs[0],
			CodeVerifier: testVerifier,
		})
		if err != nil {
			t.Fatalf("exchange: %v", err)
		}
		return h, tokens
	}

	t.Run("the access token", func(t *testing.T) {
		h, tokens := issue(t)
		ctx := context.Background()

		if err := h.svc.RevokeToken(ctx, tokens.AccessToken); err != nil {
			t.Fatalf("revoke: %v", err)
		}
		if _, _, err := h.conns.AuthenticateScoped(ctx, tokens.AccessToken); err == nil {
			t.Error("the access token still authenticates after revocation")
		}
		if _, err := h.svc.Refresh(ctx, mcpauth.RefreshExchange{RefreshToken: tokens.RefreshToken}); err == nil {
			t.Error("the refresh token survived revoking the access token")
		}
	})

	t.Run("the refresh token", func(t *testing.T) {
		h, tokens := issue(t)
		ctx := context.Background()

		if err := h.svc.RevokeToken(ctx, tokens.RefreshToken); err != nil {
			t.Fatalf("revoke: %v", err)
		}
		if _, _, err := h.conns.AuthenticateScoped(ctx, tokens.AccessToken); err == nil {
			t.Error("the access token survived revoking the refresh token")
		}
		if _, err := h.svc.Refresh(ctx, mcpauth.RefreshExchange{RefreshToken: tokens.RefreshToken}); err == nil {
			t.Error("the refresh token still works after revocation")
		}
	})

	// An unknown token is not an error the endpoint reports: telling a caller
	// apart from a real one would confirm which tokens exist.
	t.Run("an unknown token is not found rather than fatal", func(t *testing.T) {
		h := newHarness(t)
		err := h.svc.RevokeToken(context.Background(), "nk_nothing_like_a_real_token")
		if err != nil && !apperr.Is(err, apperr.ErrNotFound) {
			t.Errorf("revoking an unknown token returned %v, want ErrNotFound or nil", err)
		}
	})
}
