package strava_test

import (
	"context"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/fitness/strava"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/shared/secret"
	"github.com/NorthAIProject/north-client/internal/users"
)

const (
	accessToken  = "strava-access-0123456789abcdef"
	refreshToken = "strava-refresh-fedcba9876543210"
)

func sealer(t *testing.T, id uint8) *secret.Sealer {
	t.Helper()

	material := make([]byte, secret.KeySize)
	if _, err := rand.Read(material); err != nil {
		t.Fatalf("random key: %v", err)
	}
	s, err := secret.NewSealer(secret.Key{ID: id, Material: material})
	if err != nil {
		t.Fatalf("new sealer: %v", err)
	}
	return s
}

func seedUser(t *testing.T, pool *pgxpool.Pool, email string) users.User {
	t.Helper()

	u, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        email,
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Test",
		Timezone:     "UTC",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u
}

func connect(t *testing.T, repo *strava.Repository, userID uuid.UUID) {
	t.Helper()

	if _, err := repo.Upsert(context.Background(), userID, 42, accessToken, refreshToken,
		time.Now().Add(time.Hour), "read,activity:read"); err != nil {
		t.Fatalf("upsert: %v", err)
	}
}

// The debt migration 00022 promised: the tokens must not be readable in the
// row once a key is configured.
func TestTokensAreSealedAtRest(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool, "sealed@north.test")
	repo := strava.NewRepository(pool, sealer(t, 1))
	ctx := context.Background()

	connect(t, repo, user.ID)

	var plainAccess, plainRefresh string
	var sealedAccess, sealedRefresh []byte
	err := pool.QueryRow(ctx,
		`SELECT access_token, refresh_token, access_token_sealed, refresh_token_sealed
		 FROM strava_connections WHERE user_id = $1`, user.ID).
		Scan(&plainAccess, &plainRefresh, &sealedAccess, &sealedRefresh)
	if err != nil {
		t.Fatalf("read row: %v", err)
	}

	if plainAccess != "" || plainRefresh != "" {
		t.Fatal("the plaintext columns still hold a token")
	}
	if len(sealedAccess) == 0 || len(sealedRefresh) == 0 {
		t.Fatal("the sealed columns are empty")
	}
	if strings.Contains(string(sealedAccess), accessToken) {
		t.Fatal("the sealed column contains the plaintext token")
	}

	// And they come back intact, or the integration is broken rather than
	// secured.
	conn, err := repo.Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if conn.AccessToken != accessToken || conn.RefreshToken != refreshToken {
		t.Fatal("the tokens did not survive the round trip")
	}
}

// A deployment with no ENCRYPTION_KEY keeps working exactly as before. Losing
// the integration would be a worse answer than the debt it already carries.
func TestWithoutAKeyTheBehaviourIsUnchanged(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool, "plain@north.test")
	repo := strava.NewRepository(pool, nil)
	ctx := context.Background()

	connect(t, repo, user.ID)

	conn, err := repo.Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if conn.AccessToken != accessToken {
		t.Fatalf("access token = %q", conn.AccessToken)
	}

	var sealed []byte
	if err := pool.QueryRow(ctx,
		`SELECT access_token_sealed FROM strava_connections WHERE user_id = $1`, user.ID).Scan(&sealed); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if len(sealed) != 0 {
		t.Fatal("a sealed value was written with no key configured")
	}
}

// Rows written before the key existed must keep working, and must re-seal
// themselves the next time the token is refreshed — which is what makes the
// plaintext drain without a backfill job.
func TestAPlaintextRowIsReadableAndResealsOnRefresh(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool, "legacy@north.test")
	ctx := context.Background()

	// Written by the old code path, before a key existed.
	connect(t, strava.NewRepository(pool, nil), user.ID)

	// The process now has a key.
	repo := strava.NewRepository(pool, sealer(t, 1))

	conn, err := repo.Get(ctx, user.ID)
	if err != nil {
		t.Fatalf("the legacy row could not be read: %v", err)
	}
	if conn.AccessToken != accessToken {
		t.Fatalf("access token = %q", conn.AccessToken)
	}

	if _, err := repo.UpdateTokens(ctx, user.ID, "refreshed-access", "refreshed-refresh", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("update tokens: %v", err)
	}

	var plain string
	var sealed []byte
	if err := pool.QueryRow(ctx,
		`SELECT access_token, access_token_sealed FROM strava_connections WHERE user_id = $1`,
		user.ID).Scan(&plain, &sealed); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if plain != "" {
		t.Fatalf("the plaintext column survived a refresh: %q", plain)
	}
	if len(sealed) == 0 {
		t.Fatal("the refresh did not seal the new token")
	}
}

// The row-swap defence, end to end: the sealer binds each token to its owner.
func TestAConnectionCopiedToAnotherUserWillNotOpen(t *testing.T) {
	pool := testdb.New(t)
	owner := seedUser(t, pool, "owner@north.test")
	stranger := seedUser(t, pool, "stranger@north.test")
	repo := strava.NewRepository(pool, sealer(t, 1))
	ctx := context.Background()

	connect(t, repo, owner.ID)

	if _, err := pool.Exec(ctx,
		`INSERT INTO strava_connections
		   (user_id, athlete_id, access_token, refresh_token, access_token_sealed, refresh_token_sealed, expires_at, scopes)
		 SELECT $1, athlete_id, access_token, refresh_token, access_token_sealed, refresh_token_sealed, expires_at, scopes
		 FROM strava_connections WHERE user_id = $2`, stranger.ID, owner.ID); err != nil {
		t.Fatalf("copy row: %v", err)
	}

	if _, err := repo.Get(ctx, stranger.ID); err == nil {
		t.Fatal("another user's tokens opened; they would be syncing the owner's Strava account")
	}
	if _, err := repo.Get(ctx, owner.ID); err != nil {
		t.Fatalf("the owner's own connection stopped working: %v", err)
	}
}

// Reconnecting after losing the key would silently replace an encrypted
// credential with a plaintext one, so reading refuses instead.
func TestASealedRowRefusesToOpenWithoutAKey(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool, "keyless@north.test")
	ctx := context.Background()

	connect(t, strava.NewRepository(pool, sealer(t, 1)), user.ID)

	if _, err := strava.NewRepository(pool, nil).Get(ctx, user.ID); err == nil {
		t.Fatal("a sealed row was read by a process with no key")
	}
}

func TestARotatedAwayKeyFailsRatherThanReturningNonsense(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool, "rotated@north.test")
	ctx := context.Background()

	connect(t, strava.NewRepository(pool, sealer(t, 1)), user.ID)

	_, err := strava.NewRepository(pool, sealer(t, 2)).Get(ctx, user.ID)
	if err == nil {
		t.Fatal("a token opened under a key that never sealed it")
	}
	if strings.Contains(err.Error(), accessToken) {
		t.Errorf("the error names the token: %q", err)
	}
}

// RouteTotals feeds the coach's weekly training summary, which states distance
// and climb as fact. A window that leaks a neighbouring week, or another
// person's rides, is a wrong claim in a prompt rather than a wrong pixel.

func saveActivity(t *testing.T, repo *strava.Repository, userID uuid.UUID, id int64, start time.Time, distanceM, climbM float64) {
	t.Helper()

	err := repo.SaveActivity(context.Background(), userID, strava.Activity{
		StravaID:       id,
		Name:           "Morning Run",
		SportType:      "Run",
		StartDate:      start,
		DistanceM:      distanceM,
		MovingTimeS:    1800,
		ElapsedTimeS:   1800,
		ElevationGainM: climbM,
	})
	if err != nil {
		t.Fatalf("save activity: %v", err)
	}
}

func TestRouteTotalsSumsOnlyTheRequestedWindow(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool, "routes@north.test")
	repo := strava.NewRepository(pool, nil)

	since := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	until := since.AddDate(0, 0, 7)

	saveActivity(t, repo, user.ID, 1, since.Add(-time.Hour), 5000, 100)   // before
	saveActivity(t, repo, user.ID, 2, since, 10000, 200)                  // on the open edge
	saveActivity(t, repo, user.ID, 3, since.AddDate(0, 0, 3), 12000, 300) // inside
	saveActivity(t, repo, user.ID, 4, until, 8000, 400)                   // on the closed edge

	got, err := repo.RouteTotals(context.Background(), user.ID, since, until)
	if err != nil {
		t.Fatalf("route totals: %v", err)
	}

	if got.Activities != 2 {
		t.Errorf("Activities = %d, want the two inside the half-open window", got.Activities)
	}
	if got.DistanceM != 22000 {
		t.Errorf("DistanceM = %v, want 22000", got.DistanceM)
	}
	if got.ElevationM != 500 {
		t.Errorf("ElevationM = %v, want 500", got.ElevationM)
	}
}

func TestRouteTotalsOfAQuietWeekIsZeroRatherThanAnError(t *testing.T) {
	pool := testdb.New(t)
	user := seedUser(t, pool, "quiet@north.test")

	// "They rode nothing" is an answer. The caller renders it by saying
	// nothing, not by handling a not-found.
	since := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	got, err := strava.NewRepository(pool, nil).RouteTotals(context.Background(), user.ID, since, since.AddDate(0, 0, 7))
	if err != nil {
		t.Fatalf("route totals: %v", err)
	}

	if got != (activity.RouteTotals{}) {
		t.Fatalf("RouteTotals = %+v, want a zero struct", got)
	}
}

func TestRouteTotalsIgnoresAnotherPersonsActivities(t *testing.T) {
	pool := testdb.New(t)
	repo := strava.NewRepository(pool, nil)
	mine := seedUser(t, pool, "mine@north.test")
	theirs := seedUser(t, pool, "theirs@north.test")

	since := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	saveActivity(t, repo, mine.ID, 1, since.AddDate(0, 0, 1), 10000, 100)
	saveActivity(t, repo, theirs.ID, 2, since.AddDate(0, 0, 1), 90000, 900)

	got, err := repo.RouteTotals(context.Background(), mine.ID, since, since.AddDate(0, 0, 7))
	if err != nil {
		t.Fatalf("route totals: %v", err)
	}

	if got.DistanceM != 10000 {
		t.Fatalf("DistanceM = %v, want only my own 10000", got.DistanceM)
	}
}
