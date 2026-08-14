package aicreds_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/aicreds"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/secret"
	"github.com/NorthAIProject/north-client/internal/users"
)

const testKey = "sk-or-v1-0123456789abcdef0123456789abcdef"

func sealer(t *testing.T, ids ...uint8) *secret.Sealer {
	t.Helper()

	keys := make([]secret.Key, 0, len(ids))
	for _, id := range ids {
		material := make([]byte, secret.KeySize)
		if _, err := rand.Read(material); err != nil {
			t.Fatalf("random key: %v", err)
		}
		keys = append(keys, secret.Key{ID: id, Material: material})
	}

	s, err := secret.NewSealer(keys...)
	if err != nil {
		t.Fatalf("new sealer: %v", err)
	}
	return s
}

func newService(t *testing.T, s *secret.Sealer) (*aicreds.Service, *pgxpool.Pool, users.User) {
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
		t.Fatalf("create user: %v", err)
	}

	// Never the live verifier: a test suite that reaches five vendors is slow,
	// flaky, and would need real keys to mean anything. Tests that care about
	// verification install their own stub with WithVerifier.
	svc := aicreds.NewService(aicreds.NewRepository(pool), s, nil).WithVerifier(&stubVerifier{})

	return svc, pool, user
}

func save(t *testing.T, svc *aicreds.Service, userID uuid.UUID, in aicreds.Input) aicreds.Credential {
	t.Helper()

	cred, err := svc.Save(context.Background(), userID, in)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	return cred
}

// Reflect over every field: a future addition that could hold the key would
// otherwise slip past a test that named the fields it knew about.
func TestNoFieldOfACredentialCanHoldTheKey(t *testing.T) {
	svc, _, user := newService(t, sealer(t, 1))

	cred := save(t, svc, user.ID, aicreds.Input{Provider: "openrouter", APIKey: testKey})

	v := reflect.ValueOf(cred)
	for i := range v.NumField() {
		field := v.Field(i)
		if field.Kind() != reflect.String {
			continue
		}
		if strings.Contains(field.String(), testKey) {
			t.Fatalf("field %s contains the key", v.Type().Field(i).Name)
		}
	}

	if cred.KeyHint != testKey[len(testKey)-4:] {
		t.Errorf("key hint = %q, want the last four characters", cred.KeyHint)
	}
}

func TestTheStoredColumnIsCiphertext(t *testing.T) {
	svc, pool, user := newService(t, sealer(t, 1))
	ctx := context.Background()

	save(t, svc, user.ID, aicreds.Input{Provider: "openrouter", APIKey: testKey})

	var stored []byte
	if err := pool.QueryRow(ctx,
		`SELECT api_key FROM user_ai_credentials WHERE user_id = $1`, user.ID).Scan(&stored); err != nil {
		t.Fatalf("read row: %v", err)
	}

	if bytes.Contains(stored, []byte(testKey)) {
		t.Fatal("the stored column contains the plaintext key")
	}
	if len(stored) < 30 {
		t.Fatalf("the stored value is %d bytes, too short to be sealed", len(stored))
	}
}

func TestForBuildsAClientFromTheStoredKey(t *testing.T) {
	svc, _, user := newService(t, sealer(t, 1))
	ctx := context.Background()

	save(t, svc, user.ID, aicreds.Input{Provider: "openrouter", APIKey: testKey})

	client, err := svc.For(ctx, user.ID)
	if err != nil {
		t.Fatalf("for: %v", err)
	}
	if client == nil {
		t.Fatal("no client was built from a stored credential")
	}
	if client.Name() != "openrouter" {
		t.Errorf("client name = %q, want openrouter", client.Name())
	}
}

// The distinction the coach's whole fallback rests on: no credential is not an
// error, it is the normal state of most accounts.
func TestForReturnsNothingWhenThereIsNoCredential(t *testing.T) {
	svc, _, user := newService(t, sealer(t, 1))

	client, err := svc.For(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("for: %v", err)
	}
	if client != nil {
		t.Fatal("a client was returned for a user who has no key")
	}
}

// A deployment with no ENCRYPTION_KEY must behave as though the feature does
// not exist, not store keys in the clear.
func TestWithoutASealerNothingCanBeStored(t *testing.T) {
	svc, _, user := newService(t, nil)
	ctx := context.Background()

	if svc.Enabled() {
		t.Fatal("the service reports itself enabled with no sealer")
	}

	if _, err := svc.Save(ctx, user.ID, aicreds.Input{Provider: "openrouter", APIKey: testKey}); err == nil {
		t.Fatal("a key was saved with no sealer configured")
	}

	client, err := svc.For(ctx, user.ID)
	if err != nil || client != nil {
		t.Fatalf("for = (%v, %v), want (nil, nil) when the feature is off", client, err)
	}
}

// The row-swap test, end to end: the sealer binds the ciphertext to its owner,
// so a row copied between users fails rather than serving the wrong key.
func TestACredentialCopiedToAnotherUserWillNotOpen(t *testing.T) {
	s := sealer(t, 1)
	svc, pool, owner := newService(t, s)
	ctx := context.Background()

	userSvc := users.NewService(users.NewRepository(pool))
	stranger, err := userSvc.Register(ctx, users.Registration{
		Email:        "stranger@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Stranger",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatalf("create stranger: %v", err)
	}

	save(t, svc, owner.ID, aicreds.Input{Provider: "openrouter", APIKey: testKey})

	// Exactly what a leaked backup plus a careless UPDATE would do.
	if _, err := pool.Exec(ctx,
		`INSERT INTO user_ai_credentials (user_id, provider, api_key, key_hint, model)
		 SELECT $1, provider, api_key, key_hint, model FROM user_ai_credentials WHERE user_id = $2`,
		stranger.ID, owner.ID); err != nil {
		t.Fatalf("copy row: %v", err)
	}

	if _, err := svc.For(ctx, stranger.ID); err == nil {
		t.Fatal("another user's ciphertext opened; they would be served by the owner's key")
	}

	// And the owner is unaffected.
	if _, err := svc.For(ctx, owner.ID); err != nil {
		t.Fatalf("the owner's own credential stopped working: %v", err)
	}
}

func TestAKeySealedUnderADroppedKeyFailsLoudly(t *testing.T) {
	svc, pool, user := newService(t, sealer(t, 1))
	ctx := context.Background()

	save(t, svc, user.ID, aicreds.Input{Provider: "openrouter", APIKey: testKey})

	// The same rows, a process holding a different key.
	rotated := aicreds.NewService(aicreds.NewRepository(pool), sealer(t, 2), nil)

	client, err := rotated.For(ctx, user.ID)
	if err == nil {
		t.Fatal("a credential opened under a key that never sealed it")
	}
	if client != nil {
		t.Fatal("a client was returned alongside an error")
	}
	if strings.Contains(err.Error(), testKey) {
		t.Errorf("the error names the key: %q", err)
	}
}

// The model-only save: the field renders empty on every load, so blank must
// mean "keep what is stored" rather than "clear it".
func TestSavingWithABlankKeyKeepsTheStoredOne(t *testing.T) {
	svc, _, user := newService(t, sealer(t, 1))
	ctx := context.Background()

	save(t, svc, user.ID, aicreds.Input{Provider: "openrouter", APIKey: testKey, Model: "anthropic/claude-sonnet-4.5"})

	updated := save(t, svc, user.ID, aicreds.Input{Provider: "openrouter", Model: "openai/gpt-4.1"})
	if updated.Model != "openai/gpt-4.1" {
		t.Fatalf("model = %q, want the new one", updated.Model)
	}
	if updated.KeyHint != testKey[len(testKey)-4:] {
		t.Fatalf("key hint = %q, want the original key still stored", updated.KeyHint)
	}

	client, err := svc.For(ctx, user.ID)
	if err != nil || client == nil {
		t.Fatalf("the stored key stopped working after a model-only save: %v", err)
	}
}

func TestSaveValidates(t *testing.T) {
	svc, _, user := newService(t, sealer(t, 1))
	ctx := context.Background()

	cases := map[string]struct {
		in    aicreds.Input
		field string
	}{
		"unknown provider":   {aicreds.Input{Provider: "anthropic", APIKey: testKey}, "provider"},
		"no key at all":      {aicreds.Input{Provider: "openrouter"}, "api_key"},
		"absurd key":         {aicreds.Input{Provider: "openrouter", APIKey: strings.Repeat("k", 5000)}, "api_key"},
		"absurd model":       {aicreds.Input{Provider: "openrouter", APIKey: testKey, Model: strings.Repeat("m", 300)}, "model"},
		"hermes without url": {aicreds.Input{Provider: "hermes", APIKey: testKey}, "base_url"},
		"hermes loopback":    {aicreds.Input{Provider: "hermes", APIKey: testKey, BaseURL: "http://127.0.0.1:8642/v1"}, "base_url"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Save(ctx, user.ID, tc.in)

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

func TestHermesCredentialKeepsItsOwnURL(t *testing.T) {
	svc, _, user := newService(t, sealer(t, 1))
	ctx := context.Background()

	cred := save(t, svc, user.ID, aicreds.Input{
		Provider: "hermes",
		APIKey:   testKey,
		BaseURL:  "http://hermes-vps-2.tail562587.ts.net:8642",
	})
	if cred.BaseURL != "http://hermes-vps-2.tail562587.ts.net:8642/v1" {
		t.Fatalf("base_url = %q", cred.BaseURL)
	}

	client, err := svc.For(ctx, user.ID)
	if err != nil || client == nil {
		t.Fatalf("for = (%v, %v)", client, err)
	}
	if client.Name() != "hermes" {
		t.Fatalf("client = %q", client.Name())
	}
}

func TestDeletePutsTheUserBackOnNorthsProviders(t *testing.T) {
	svc, _, user := newService(t, sealer(t, 1))
	ctx := context.Background()

	save(t, svc, user.ID, aicreds.Input{Provider: "openrouter", APIKey: testKey})
	if err := svc.Delete(ctx, user.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	client, err := svc.For(ctx, user.ID)
	if err != nil || client != nil {
		t.Fatalf("for = (%v, %v) after delete, want (nil, nil)", client, err)
	}
	if _, err := svc.Get(ctx, user.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Fatalf("get = %v, want ErrNotFound", err)
	}
}

// A cached client must not outlive the credential it was built from, or
// removing a key would keep working until the TTL expired.
func TestDeleteInvalidatesTheCachedClient(t *testing.T) {
	svc, _, user := newService(t, sealer(t, 1))
	ctx := context.Background()

	save(t, svc, user.ID, aicreds.Input{Provider: "openrouter", APIKey: testKey})
	if _, err := svc.For(ctx, user.ID); err != nil {
		t.Fatalf("for: %v", err)
	}

	if err := svc.Delete(ctx, user.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if client, _ := svc.For(ctx, user.ID); client != nil {
		t.Fatal("a cached client survived the credential being deleted")
	}
}

// Changing provider must change which client is built, not serve the previous
// one until a timer expires.
func TestSavingANewProviderReplacesTheCachedClient(t *testing.T) {
	svc, _, user := newService(t, sealer(t, 1))
	ctx := context.Background()

	save(t, svc, user.ID, aicreds.Input{Provider: "openrouter", APIKey: testKey})
	first, err := svc.For(ctx, user.ID)
	if err != nil {
		t.Fatalf("for: %v", err)
	}

	save(t, svc, user.ID, aicreds.Input{Provider: "xai", APIKey: "xai-0123456789abcdef"})
	second, err := svc.For(ctx, user.ID)
	if err != nil {
		t.Fatalf("for: %v", err)
	}

	if first.Name() == second.Name() {
		t.Fatalf("still serving %q after switching provider", second.Name())
	}
	if second.Name() != "xai" {
		t.Errorf("client name = %q, want xai", second.Name())
	}
}

func TestNoteFailureIsVisibleAndClearedByANewKey(t *testing.T) {
	svc, _, user := newService(t, sealer(t, 1))
	ctx := context.Background()

	save(t, svc, user.ID, aicreds.Input{Provider: "openrouter", APIKey: testKey})
	svc.NoteFailure(ctx, user.ID, "Your provider rejected the key.")

	cred, err := svc.Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !cred.Failing() {
		t.Fatal("a recorded failure is not visible on the credential")
	}
	if cred.LastErrorAt == nil {
		t.Error("a recorded failure has no timestamp")
	}

	// Replacing the key clears the complaint, or the page would go on saying
	// the credential was rejected after it had been fixed.
	updated := save(t, svc, user.ID, aicreds.Input{Provider: "openrouter", APIKey: testKey + "new"})
	if updated.Failing() {
		t.Fatalf("last_error = %q, want it cleared by a new key", updated.LastError)
	}
}

func TestDeletingAUserRemovesTheirCredential(t *testing.T) {
	svc, pool, user := newService(t, sealer(t, 1))
	ctx := context.Background()

	save(t, svc, user.ID, aicreds.Input{Provider: "openrouter", APIKey: testKey})

	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, user.ID); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM user_ai_credentials WHERE user_id = $1`, user.ID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("%d credentials survived the user being deleted", count)
	}
}
