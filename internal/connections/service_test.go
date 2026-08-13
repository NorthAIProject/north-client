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

func TestRevokedTokenStopsWorking(t *testing.T) {
	svc, _, user := newService(t)
	ctx := context.Background()

	issued, err := svc.Issue(ctx, user.ID, "Laptop", connections.ClientHermes)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := svc.Authenticate(ctx, issued.Token); err != nil {
		t.Fatalf("authenticate before revoke: %v", err)
	}

	if err := svc.Revoke(ctx, issued.ID, user.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := svc.Authenticate(ctx, issued.Token); !apperr.Is(err, apperr.ErrUnauthenticated) {
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
