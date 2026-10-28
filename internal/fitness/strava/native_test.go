package strava_test

import (
	"context"
	"crypto/sha256"
	"net/url"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/fitness/strava"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func stateHash(state string) []byte {
	sum := sha256.Sum256([]byte(state))
	return sum[:]
}

// A connection begun in the app sends Strava back to the public API callback,
// and its state is the only thing that says whose connection it is.
func TestNativeConnectStateBelongsToItsPersonAndIsUsedOnce(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "native-strava@example.com")
	repo := strava.NewRepository(pool, nil)
	svc := strava.NewService(strava.Options{Repository: repo, ClientID: "123", ClientSecret: "shh", BaseURL: "https://kheprios.test"})

	consent, err := svc.BeginNativeConnect(ctx, user.ID, "https://kheprios.test/")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	parsed, err := url.Parse(consent)
	if err != nil {
		t.Fatalf("parse %q: %v", consent, err)
	}
	if got := parsed.Query().Get("redirect_uri"); got != "https://kheprios.test"+strava.NativeCallbackPath {
		t.Errorf("redirect_uri = %q, want the native callback", got)
	}
	state := parsed.Query().Get("state")

	owner, err := repo.TakeOAuthState(ctx, stateHash(state))
	if err != nil || owner != user.ID {
		t.Fatalf("take = %v, %v; want %v", owner, err, user.ID)
	}
	if _, err := repo.TakeOAuthState(ctx, stateHash(state)); !apperr.Is(err, apperr.ErrNotFound) {
		t.Errorf("second take = %v, want ErrNotFound", err)
	}
}

func TestNativeConnectRefusesExpiredAndUnknownStates(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := seedUser(t, pool, "native-strava-expired@example.com")
	repo := strava.NewRepository(pool, nil)

	if err := repo.SaveOAuthState(ctx, stateHash("old"), user.ID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := repo.TakeOAuthState(ctx, stateHash("old")); !apperr.Is(err, apperr.ErrNotFound) {
		t.Errorf("expired take = %v, want ErrNotFound", err)
	}

	svc := strava.NewService(strava.Options{Repository: repo, ClientID: "123", ClientSecret: "shh"})
	if err := svc.FinishNativeConnect(ctx, "never-issued", "code"); !apperr.Is(err, apperr.ErrNotFound) {
		t.Errorf("unknown state = %v, want ErrNotFound", err)
	}
}

// A connection begun on the web returns to the web app's own callback, and
// finishing it refuses a state that was issued to somebody else.
func TestWebConnectStateMustBelongToTheSignedInPerson(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	owner := seedUser(t, pool, "web-strava-owner@example.com")
	other := seedUser(t, pool, "web-strava-other@example.com")
	repo := strava.NewRepository(pool, nil)
	svc := strava.NewService(strava.Options{Repository: repo, ClientID: "123", ClientSecret: "shh", BaseURL: "https://kheprios.test"})

	callback := "https://app.kheprios.test" + strava.WebCallbackPath
	consent, err := svc.BeginWebConnect(ctx, owner.ID, callback)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	parsed, err := url.Parse(consent)
	if err != nil {
		t.Fatalf("parse %q: %v", consent, err)
	}
	if got := parsed.Query().Get("redirect_uri"); got != callback {
		t.Errorf("redirect_uri = %q, want %q", got, callback)
	}
	state := parsed.Query().Get("state")

	if err := svc.FinishWebConnect(ctx, other.ID, state, "code"); !apperr.Is(err, apperr.ErrForbidden) {
		t.Errorf("finish as another person = %v, want ErrForbidden", err)
	}
	// The refused attempt used the state up: it cannot be replayed by its owner.
	if err := svc.FinishWebConnect(ctx, owner.ID, state, "code"); !apperr.Is(err, apperr.ErrNotFound) {
		t.Errorf("replay = %v, want ErrNotFound", err)
	}
	if err := svc.FinishWebConnect(ctx, owner.ID, "", "code"); !apperr.Is(err, apperr.ErrNotFound) {
		t.Errorf("empty state = %v, want ErrNotFound", err)
	}
}
