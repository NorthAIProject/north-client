// Package auth owns credentials and sessions: proving who a request belongs to
// and keeping that proof safe at rest.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	authdb "github.com/NorthAIProject/north-client/internal/auth/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// sessionTokenBytes is the entropy of a session token. 32 bytes is far beyond
// brute force and costs nothing.
const sessionTokenBytes = 32

// touchInterval throttles sliding-expiry writes. Extending the session on every
// single request would mean a database write per page view for no real benefit.
const touchInterval = time.Hour

// SessionStore issues, resolves, and revokes sessions.
//
// The database stores only the SHA-256 of each token. The raw token exists in
// the user's cookie and nowhere else, so a leaked backup cannot be replayed as
// a live login. SHA-256 rather than bcrypt is correct here: the token is 256
// bits of uniform randomness, so there is no dictionary to defend against, and
// this lookup runs on every authenticated request.
type SessionStore struct {
	q        *authdb.Queries
	lifetime time.Duration
}

func NewSessionStore(pool *pgxpool.Pool, lifetime time.Duration) *SessionStore {
	return &SessionStore{q: authdb.New(pool), lifetime: lifetime}
}

// Session is a resolved, still-valid session.
type Session struct {
	User      users.User
	ExpiresAt time.Time
}

// Metadata describes the client that created a session, for display on a future
// "where you are signed in" screen.
type Metadata struct {
	UserAgent string
	IP        string
}

// Create issues a new session and returns the raw token to put in a cookie.
// The token is returned exactly once and is unrecoverable afterwards.
func (s *SessionStore) Create(ctx context.Context, userID uuid.UUID, meta Metadata) (string, time.Time, error) {
	raw := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", time.Time{}, apperr.Wrap(err, "generate session token")
	}

	token := base64.RawURLEncoding.EncodeToString(raw)
	expiresAt := time.Now().Add(s.lifetime)

	params := authdb.CreateSessionParams{
		TokenHash: hashToken(token),
		UserID:    userID,
		ExpiresAt: expiresAt,
	}
	if meta.UserAgent != "" {
		ua := truncate(meta.UserAgent, 500)
		params.UserAgent = &ua
	}
	if addr, err := netip.ParseAddr(meta.IP); err == nil {
		params.Ip = &addr
	}

	if _, err := s.q.CreateSession(ctx, params); err != nil {
		return "", time.Time{}, apperr.Wrap(err, "create session")
	}

	return token, expiresAt, nil
}

// Resolve looks up a session by its raw token.
//
// It returns ErrUnauthenticated for a token that is unknown, expired, or
// malformed. The three are not distinguished: telling a caller which one it was
// only helps someone probing for valid tokens.
func (s *SessionStore) Resolve(ctx context.Context, token string) (Session, error) {
	if token == "" {
		return Session{}, apperr.ErrUnauthenticated
	}

	row, err := s.q.GetSessionUser(ctx, hashToken(token))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, apperr.ErrUnauthenticated
		}
		return Session{}, apperr.Wrap(err, "resolve session")
	}

	session := Session{User: userFromDB(row.User), ExpiresAt: row.ExpiresAt}

	// Sliding expiry: an active user should not be logged out mid-conversation.
	// Throttled so this is not a write on every request.
	if time.Since(row.LastSeenAt) > touchInterval {
		extended := time.Now().Add(s.lifetime)
		if err := s.q.TouchSession(ctx, authdb.TouchSessionParams{
			TokenHash: hashToken(token),
			ExpiresAt: extended,
		}); err == nil {
			session.ExpiresAt = extended
		}
		// A failed touch is not worth failing the request over: the session is
		// still valid, it just expires on the original schedule.
	}

	return session, nil
}

// Revoke ends a single session, for an explicit sign-out.
func (s *SessionStore) Revoke(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return apperr.Wrap(s.q.DeleteSession(ctx, hashToken(token)), "revoke session")
}

// RevokeAll ends every session for a user. Called on password change, so that
// changing a password actually locks out whoever else was signed in.
func (s *SessionStore) RevokeAll(ctx context.Context, userID uuid.UUID) error {
	return apperr.Wrap(s.q.DeleteUserSessions(ctx, userID), "revoke all sessions")
}

// PurgeExpired deletes expired rows and reports how many. Expired sessions are
// already rejected on read; this only stops the table growing forever.
func (s *SessionStore) PurgeExpired(ctx context.Context) (int64, error) {
	n, err := s.q.DeleteExpiredSessions(ctx)
	return n, apperr.Wrap(err, "purge expired sessions")
}

// Lifetime is the configured session duration, used to set cookie expiry.
func (s *SessionStore) Lifetime() time.Duration { return s.lifetime }

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// userFromDB converts the session join row's user into the domain type.
// internal/users cannot do this conversion because the row type belongs to this
// slice's generated package.
func userFromDB(row authdb.User) users.User {
	u := users.User{
		ID:          row.ID,
		Email:       row.Email,
		DisplayName: row.DisplayName,
		Timezone:    row.Timezone,
		Tier:        users.Tier(row.Tier),
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
	if row.CoachingStyle != nil {
		u.CoachingStyle = *row.CoachingStyle
	}
	u.CoachingTone = users.Tone(row.CoachingTone)
	u.OnboardedAt = row.OnboardedAt
	return u
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
