package connections_test

import (
	"context"
	"crypto/sha256"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/connections"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

func newService(t *testing.T) (*connections.Service, *pgxpool.Pool, users.User) {
	t.Helper()

	pool := testdb.New(t)
	userSvc := users.NewService(users.NewRepository(pool))
	user := register(t, userSvc, "fernando@north.test")

	svc := connections.NewService(connections.NewRepository(pool), userSvc, "https://north.test")
	return svc, pool, user
}

func register(t *testing.T, svc *users.Service, email string) users.User {
	t.Helper()

	user, err := svc.Register(context.Background(), users.Registration{
		Email:        email,
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Fernando Correia",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("create user %s: %v", email, err)
	}
	return user
}

func TestIssueReturnsTheTokenOnceAndStoresOnlyItsHash(t *testing.T) {
	svc, pool, user := newService(t)
	ctx := context.Background()

	issued, err := svc.Issue(ctx, user.ID, "Laptop", connections.ClientClaudeCode)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	if !strings.HasPrefix(issued.Token, "nk_") {
		t.Fatalf("token = %q, want an nk_ prefix", issued.Token)
	}
	if !strings.HasPrefix(issued.Token, issued.TokenPrefix) {
		t.Fatalf("stored prefix %q is not a prefix of the token", issued.TokenPrefix)
	}
	if len(issued.TokenPrefix) >= len(issued.Token) {
		t.Fatal("the stored prefix is the whole token; it must be a fragment")
	}

	// The assertion that would have caught the plaintext-token debt in
	// strava_connections: no text column anywhere holds the credential.
	var hash []byte
	var name, prefix string
	err = pool.QueryRow(ctx,
		`SELECT token_hash, name, token_prefix FROM agent_connections WHERE id = $1`,
		issued.ID).Scan(&hash, &name, &prefix)
	if err != nil {
		t.Fatalf("read row: %v", err)
	}

	want := sha256.Sum256([]byte(issued.Token))
	if string(hash) != string(want[:]) {
		t.Fatal("stored hash is not sha256 of the issued token")
	}
	for _, col := range []string{name, prefix} {
		if strings.Contains(col, issued.Token) {
			t.Fatalf("a text column contains the whole token: %q", col)
		}
	}
}

func TestIssuedTokenAuthenticatesAsItsOwner(t *testing.T) {
	svc, _, user := newService(t)
	ctx := context.Background()

	issued, err := svc.Issue(ctx, user.ID, "Laptop", connections.ClientCodex)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	got, err := svc.Authenticate(ctx, issued.Token)
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if got.ID != user.ID {
		t.Fatalf("authenticated as %s, want %s", got.ID, user.ID)
	}
}

// Every rejection must be the same rejection. A message that distinguished
// "revoked" from "unknown" would confirm that a guessed token once existed,
// which is the single bit an attacker was after.
func TestAuthenticateRejectionsAreIndistinguishable(t *testing.T) {
	svc, _, user := newService(t)
	ctx := context.Background()

	issued, err := svc.Issue(ctx, user.ID, "Laptop", connections.ClientOther)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	revoked, err := svc.Issue(ctx, user.ID, "Old laptop", connections.ClientOther)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if err := svc.Revoke(ctx, revoked.ID, user.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	// A well-formed token that was never issued: same shape, different bytes.
	neverIssued := issued.Token[:len(issued.Token)-4] + "AAAA"

	cases := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"no prefix", "garbage"},
		{"prefix only", "nk_"},
		{"never issued", neverIssued},
		{"revoked", revoked.Token},
	}

	var messages []string
	for _, tc := range cases {
		_, err := svc.Authenticate(ctx, tc.token)
		if err == nil {
			t.Fatalf("%s: authenticate succeeded, want rejection", tc.name)
		}
		if !apperr.Is(err, apperr.ErrUnauthenticated) {
			t.Fatalf("%s: err = %v, want ErrUnauthenticated", tc.name, err)
		}
		messages = append(messages, err.Error())
	}

	for i, msg := range messages {
		if msg != messages[0] {
			t.Fatalf("case %q answers %q but case %q answers %q; every rejection must read the same",
				cases[i].name, msg, cases[0].name, messages[0])
		}
	}
}

// The IDOR test. The id comes from a form, so the query must be scoped by
// owner as well — otherwise a guessed id revokes a stranger's agent.
func TestRevokeIsScopedToTheOwner(t *testing.T) {
	pool := testdb.New(t)
	userSvc := users.NewService(users.NewRepository(pool))
	owner := register(t, userSvc, "owner@north.test")
	stranger := register(t, userSvc, "stranger@north.test")

	svc := connections.NewService(connections.NewRepository(pool), userSvc, "https://north.test")
	ctx := context.Background()

	issued, err := svc.Issue(ctx, owner.ID, "Laptop", connections.ClientClaudeCode)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	if err := svc.Revoke(ctx, issued.ID, stranger.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("stranger revoking another user's connection: err = %v, want ErrNotFound", err)
	}

	if _, err := svc.Authenticate(ctx, issued.Token); err != nil {
		t.Fatalf("the owner's token stopped working after a stranger tried to revoke it: %v", err)
	}
}

// The status poll takes a connection id straight from the URL, so Get must be
// scoped by owner as well. Unscoped, a stranger could confirm that a guessed id
// exists and watch when it was last used — a two-field leak that reveals both
// that somebody has connected an agent and roughly when they are working.
//
// It is scoped today because Get filters a list that is already
// WHERE user_id = $1. This test is here so that stays true if somebody later
// replaces it with a lookup by id, which is the obvious optimisation and the
// one that would quietly remove the predicate.
func TestGetIsScopedToTheOwner(t *testing.T) {
	pool := testdb.New(t)
	userSvc := users.NewService(users.NewRepository(pool))
	owner := register(t, userSvc, "get-owner@north.test")
	stranger := register(t, userSvc, "get-stranger@north.test")

	svc := connections.NewService(connections.NewRepository(pool), userSvc, "https://north.test")
	ctx := context.Background()

	issued, err := svc.Issue(ctx, owner.ID, "Laptop", connections.ClientClaudeCode)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	if _, strangerErr := svc.Get(ctx, issued.ID, stranger.ID); !apperr.Is(strangerErr, apperr.ErrNotFound) {
		t.Fatalf("a stranger read another user's connection: err = %v, want ErrNotFound", strangerErr)
	}

	got, err := svc.Get(ctx, issued.ID, owner.ID)
	if err != nil {
		t.Fatalf("the owner could not read their own connection: %v", err)
	}
	if got.ID != issued.ID {
		t.Fatalf("got connection %s, want %s", got.ID, issued.ID)
	}
}

// A revoked connection is gone as far as Get is concerned, which is also what
// stops the status poll rather than leaving it asking about a dead token.
func TestGetDoesNotReturnARevokedConnection(t *testing.T) {
	svc, _, user := newService(t)
	ctx := context.Background()

	issued, err := svc.Issue(ctx, user.ID, "Laptop", connections.ClientOther)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if err := svc.Revoke(ctx, issued.ID, user.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	if _, err := svc.Get(ctx, issued.ID, user.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("a revoked connection was still readable: err = %v, want ErrNotFound", err)
	}
}

func TestRevokedTokenStopsWorking(t *testing.T) {
	svc, _, user := newService(t)
	ctx := context.Background()

	issued, err := svc.Issue(ctx, user.ID, "Laptop", connections.ClientHermes)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err = svc.Authenticate(ctx, issued.Token); err != nil {
		t.Fatalf("authenticate before revoke: %v", err)
	}

	if err = svc.Revoke(ctx, issued.ID, user.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err = svc.Authenticate(ctx, issued.Token); !apperr.Is(err, apperr.ErrUnauthenticated) {
		t.Fatalf("revoked token still authenticates: err = %v", err)
	}

	list, err := svc.List(ctx, user.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("listed %d connections after revoking the only one", len(list))
	}
}

func TestTokensAreUnique(t *testing.T) {
	svc, _, user := newService(t)
	ctx := context.Background()

	seen := make(map[string]bool, 50)
	for i := 0; i < 50; i++ {
		issued, err := svc.Issue(ctx, user.ID, "Laptop", connections.ClientOther)
		if err != nil {
			t.Fatalf("issue %d: %v", i, err)
		}
		if seen[issued.Token] {
			t.Fatal("two issues produced the same token")
		}
		seen[issued.Token] = true
	}
}

// First use is written immediately: "never used" is what tells somebody their
// paste did not work, and a five-minute lie there is the difference between
// the feature working and the user giving up.
func TestFirstUseIsRecordedAndThenThrottled(t *testing.T) {
	svc, _, user := newService(t)
	ctx := context.Background()

	issued, err := svc.Issue(ctx, user.ID, "Laptop", connections.ClientClaudeCode)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if issued.Used() {
		t.Fatal("a freshly issued connection reports having been used")
	}

	if _, err := svc.Authenticate(ctx, issued.Token); err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	first := lastUsed(t, svc, user.ID, issued.ID)
	if first == nil {
		t.Fatal("last_used_at is still null after the first authenticated call")
	}

	// A second call inside the touch window must not write again, or a polling
	// agent turns a read-only endpoint into a write-heavy one.
	if _, err := svc.Authenticate(ctx, issued.Token); err != nil {
		t.Fatalf("second authenticate: %v", err)
	}
	if second := lastUsed(t, svc, user.ID, issued.ID); !second.Equal(*first) {
		t.Fatalf("last_used_at moved from %v to %v inside the touch window", first, second)
	}
}

func TestIssueValidates(t *testing.T) {
	svc, _, user := newService(t)
	ctx := context.Background()

	cases := []struct {
		name  string
		label string
		kind  connections.ClientKind
		field string
	}{
		{"blank name", "   ", connections.ClientClaudeCode, "name"},
		{"overlong name", strings.Repeat("x", 61), connections.ClientClaudeCode, "name"},
		{"unknown client", "Laptop", connections.ClientKind("emacs"), "client_kind"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Issue(ctx, user.ID, tc.label, tc.kind)

			var fieldErrs apperr.FieldErrors
			if !apperr.As(err, &fieldErrs) {
				t.Fatalf("err = %v, want FieldErrors", err)
			}
			if _, ok := fieldErrs.Messages()[tc.field]; !ok {
				t.Fatalf("errors = %v, want one on %q", fieldErrs.Messages(), tc.field)
			}
		})
	}
}

func TestDeletingAUserRemovesTheirConnections(t *testing.T) {
	svc, pool, user := newService(t)
	ctx := context.Background()

	if _, err := svc.Issue(ctx, user.ID, "Laptop", connections.ClientClaudeCode); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, user.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM agent_connections WHERE user_id = $1`, user.ID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("%d connections survived the user being deleted", count)
	}
}

func lastUsed(t *testing.T, svc *connections.Service, userID, id uuid.UUID) *time.Time {
	t.Helper()

	list, err := svc.List(context.Background(), userID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, conn := range list {
		if conn.ID == id {
			return conn.LastUsedAt
		}
	}
	t.Fatalf("connection %s is not in the list", id)
	return nil
}

// Every token issued before OAuth existed has an empty scopes and a NULL
// expires_at, and the authentication query grew a predicate on that column.
// This is the guard that the predicate did not quietly invalidate all of them.
func TestATokenWithNoExpiryStillAuthenticates(t *testing.T) {
	svc, pool, user := newService(t)
	ctx := context.Background()

	issued, err := svc.Issue(ctx, user.ID, "Laptop", connections.ClientClaudeCode)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	var (
		scopes    string
		expiresAt *time.Time
		issuance  string
	)
	if err := pool.QueryRow(ctx,
		`SELECT scopes, expires_at, issuance FROM agent_connections WHERE id = $1`,
		issued.ID,
	).Scan(&scopes, &expiresAt, &issuance); err != nil {
		t.Fatalf("read the stored row: %v", err)
	}

	if scopes != "" {
		t.Errorf("a hand-issued token stored scopes %q, want empty (full access)", scopes)
	}
	if expiresAt != nil {
		t.Errorf("a hand-issued token stored an expiry of %v, want none", expiresAt)
	}
	if issuance != "pat" {
		t.Errorf("a hand-issued token recorded issuance %q, want pat", issuance)
	}

	if _, err := svc.Authenticate(ctx, issued.Token); err != nil {
		t.Fatalf("a token with no expiry stopped authenticating: %v", err)
	}
}

// An OAuth access token lasts an hour, and the point of the predicate is that
// presenting an expired one is refused — indistinguishably from any other
// rejection, so an expired token is not an oracle for a token that once
// existed.
func TestAnExpiredTokenIsRefusedLikeAnUnknownOne(t *testing.T) {
	svc, pool, user := newService(t)
	ctx := context.Background()

	issued, err := svc.Issue(ctx, user.ID, "Laptop", connections.ClientClaudeCode)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`UPDATE agent_connections SET expires_at = now() - interval '1 minute' WHERE id = $1`,
		issued.ID,
	); err != nil {
		t.Fatalf("expire the connection: %v", err)
	}

	_, expired := svc.Authenticate(ctx, issued.Token)
	if !apperr.Is(expired, apperr.ErrUnauthenticated) {
		t.Fatalf("an expired token returned %v, want ErrUnauthenticated", expired)
	}

	// The same error as a token that never existed. A distinguishable message
	// here is the enumeration oracle the rejection path exists to avoid.
	_, unknown := svc.Authenticate(ctx, "nk_thistokenwasneverissuedatall")
	if expired.Error() != unknown.Error() {
		t.Errorf("an expired token reports %q and an unknown one %q; they must match",
			expired.Error(), unknown.Error())
	}
}

// The grant is the row, so an expired access token must still be listed. The
// settings page shows it as needing a refresh; hiding it would make a working
// connection vanish an hour after it was made.
func TestAnExpiredConnectionIsStillListed(t *testing.T) {
	svc, pool, user := newService(t)
	ctx := context.Background()

	issued, err := svc.Issue(ctx, user.ID, "Laptop", connections.ClientClaudeCode)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, expireErr := pool.Exec(ctx,
		`UPDATE agent_connections SET expires_at = now() - interval '1 minute' WHERE id = $1`,
		issued.ID,
	); expireErr != nil {
		t.Fatalf("expire the connection: %v", expireErr)
	}

	list, err := svc.List(ctx, user.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("listed %d connections, want the expired one to still be there", len(list))
	}
}

// A grant is stored in the same table as a pasted token and authenticates
// through the same path, which is what lets one revoke button cover both and
// keeps mcpserver.Authenticator a single implementation.
func TestAnOAuthGrantAuthenticatesAndCarriesItsScope(t *testing.T) {
	svc, _, user := newService(t)
	ctx := context.Background()

	issued, err := svc.IssueGrant(ctx, connections.GrantInput{
		UserID:        user.ID,
		ClientName:    "Claude Code",
		Scopes:        "north:read",
		Resource:      "https://north.test/mcp",
		OAuthClientID: "",
		TTL:           time.Hour,
	})
	if err != nil {
		t.Fatalf("issue grant: %v", err)
	}

	if !strings.HasPrefix(issued.Token, "nk_") {
		t.Errorf("an OAuth access token is %q; it must carry the nk_ prefix so "+
			"AuthenticateScoped accepts it like any other", issued.Token)
	}
	if issued.Issuance != connections.IssuanceOAuth {
		t.Errorf("issuance is %q, want oauth", issued.Issuance)
	}
	if issued.Kind != connections.ClientClaudeCode {
		t.Errorf("kind is %q, want claude_code from the client name", issued.Kind)
	}
	if issued.ExpiresAt == nil {
		t.Error("an OAuth grant stored no expiry; access tokens must expire")
	}

	gotUser, scope, err := svc.AuthenticateScoped(ctx, issued.Token)
	if err != nil {
		t.Fatalf("authenticate a granted token: %v", err)
	}
	if gotUser.ID != user.ID {
		t.Errorf("token authenticated as %s, want %s", gotUser.ID, user.ID)
	}
	if scope != "north:read" {
		t.Errorf("scope is %q, want north:read", scope)
	}
}

// Refreshing replaces the token in place. One row is one grant, so the
// settings page shows one entry however many hours it has been alive — and the
// old token must stop working the moment the new one exists.
func TestRotatingAGrantTokenReplacesTheOldOne(t *testing.T) {
	svc, _, user := newService(t)
	ctx := context.Background()

	issued, err := svc.IssueGrant(ctx, connections.GrantInput{
		UserID:     user.ID,
		ClientName: "Claude Code",
		Scopes:     "north:read_write",
		TTL:        time.Hour,
	})
	if err != nil {
		t.Fatalf("issue grant: %v", err)
	}

	rotated, err := svc.RotateGrantToken(ctx, issued.ID, time.Hour)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if rotated == issued.Token {
		t.Fatal("rotation returned the same token")
	}

	if _, _, authErr := svc.AuthenticateScoped(ctx, rotated); authErr != nil {
		t.Errorf("the rotated token does not authenticate: %v", authErr)
	}
	if _, _, staleErr := svc.AuthenticateScoped(ctx, issued.Token); !apperr.Is(staleErr, apperr.ErrUnauthenticated) {
		t.Errorf("the superseded token still authenticates (%v); rotation must retire it", staleErr)
	}

	// Still one row, and still the same one.
	list, err := svc.List(ctx, user.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("rotation left %d connections, want 1", len(list))
	}
	if list[0].ID != issued.ID {
		t.Errorf("rotation replaced the row (%s, was %s)", list[0].ID, issued.ID)
	}
	if list[0].CreatedAt != issued.CreatedAt {
		t.Error("rotation moved created_at; it must stay the date consent was given")
	}
}

// A pasted token has no refresh flow and must not acquire one: rotation is
// scoped to grants.
func TestRotatingRefusesAHandIssuedToken(t *testing.T) {
	svc, _, user := newService(t)
	ctx := context.Background()

	issued, err := svc.Issue(ctx, user.ID, "Laptop", connections.ClientClaudeCode)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	if _, err := svc.RotateGrantToken(ctx, issued.ID, time.Hour); !apperr.Is(err, apperr.ErrNotFound) {
		t.Errorf("rotating a pasted token returned %v, want ErrNotFound", err)
	}
	if _, _, err := svc.AuthenticateScoped(ctx, issued.Token); err != nil {
		t.Errorf("the pasted token stopped working after a refused rotation: %v", err)
	}
}

// The kind is derived from a name the client chose for itself, so this is the
// one place a hostile registration reaches presentation. It must land on an
// existing kind rather than widen the vocabulary.
func TestClientKindForMapsOntoTheExistingVocabulary(t *testing.T) {
	for name, want := range map[string]connections.ClientKind{
		"Claude Code":      connections.ClientClaudeCode,
		"claude-code":      connections.ClientClaudeCode,
		"Codex CLI":        connections.ClientCodex,
		"hermes":           connections.ClientHermes,
		"Claude Desktop":   connections.ClientOther,
		"claude.ai":        connections.ClientOther,
		"":                 connections.ClientOther,
		"<script>alert(1)": connections.ClientOther,
	} {
		if got := connections.ClientKindFor(name); got != want {
			t.Errorf("ClientKindFor(%q) = %q, want %q", name, got, want)
		}
	}
}
