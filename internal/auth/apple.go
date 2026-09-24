package auth

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	authdb "github.com/NorthAIProject/north-client/internal/auth/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

const appleJWKSURL = "https://appleid.apple.com/auth/keys"

// appleAuth verifies identity tokens issued for any of the app's bundle IDs.
// The Beta build is a separate app with its own bundle ID, and Apple puts the
// bundle ID in the token's audience.
type appleAuth struct {
	bundleIDs []string
}

// newAppleAuth takes a comma-separated list of bundle IDs.
func newAppleAuth(bundleIDs string) *appleAuth {
	return &appleAuth{bundleIDs: splitList(bundleIDs)}
}

type appleClaims struct {
	Email string `json:"email"`
	Nonce string `json:"nonce"`
	jwt.RegisteredClaims
}

type appleKeys struct {
	Keys []appleKey `json:"keys"`
}

type appleKey struct {
	Kid string `json:"kid"`
	Kty string `json:"kty"`
	N   string `json:"n"`
	E   string `json:"e"`
}

func (s *Service) CompleteAppleSignIn(ctx context.Context, in AppleSignInInput, meta Metadata) (users.User, string, time.Time, error) {
	if s.apple == nil || len(s.apple.bundleIDs) == 0 {
		return users.User{}, "", time.Time{}, apperr.New("apple sign-in is not configured")
	}
	claims, err := s.apple.verify(ctx, in.IdentityToken, in.Nonce)
	if err != nil {
		return users.User{}, "", time.Time{}, err
	}

	user, err := s.findOrCreateAppleUser(ctx, claims.Subject, claims.Email, in.FullName)
	if err != nil {
		return users.User{}, "", time.Time{}, err
	}
	token, expiresAt, err := s.sessions.Create(ctx, user.ID, meta)
	return user, token, expiresAt, err
}

type AppleSignInInput struct {
	IdentityToken string
	Nonce         string
	FullName      string
	Email         string
}

func (a *appleAuth) verify(ctx context.Context, rawToken, rawNonce string) (appleClaims, error) {
	if strings.TrimSpace(rawToken) == "" || strings.TrimSpace(rawNonce) == "" {
		return appleClaims{}, apperr.ErrUnauthenticated
	}
	keys, err := a.keys(ctx)
	if err != nil {
		return appleClaims{}, apperr.Wrap(err, "fetch apple signing keys")
	}

	var claims appleClaims
	_, err = jwt.ParseWithClaims(rawToken, &claims, func(token *jwt.Token) (any, error) {
		if token.Method.Alg() != jwt.SigningMethodRS256.Alg() {
			return nil, errors.New("unexpected apple signing algorithm")
		}
		kid, ok := token.Header["kid"].(string)
		if !ok {
			return nil, errors.New("apple token missing key id")
		}
		for _, key := range keys.Keys {
			if key.Kid == kid {
				return rsaKey(key)
			}
		}
		return nil, errors.New("apple signing key not found")
	}, jwt.WithIssuer("https://appleid.apple.com"), jwt.WithAudience(a.bundleIDs...))
	if err != nil || claims.Subject == "" {
		return appleClaims{}, apperr.ErrUnauthenticated
	}

	hash := sha256.Sum256([]byte(rawNonce))
	expected := hex.EncodeToString(hash[:])
	if subtle.ConstantTimeCompare([]byte(expected), []byte(claims.Nonce)) != 1 {
		return appleClaims{}, apperr.ErrUnauthenticated
	}
	return claims, nil
}

func (a *appleAuth) keys(ctx context.Context) (appleKeys, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, appleJWKSURL, nil)
	if err != nil {
		return appleKeys{}, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return appleKeys{}, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return appleKeys{}, errors.New("apple signing keys returned " + response.Status)
	}
	var keys appleKeys
	if err := json.NewDecoder(response.Body).Decode(&keys); err != nil {
		return appleKeys{}, err
	}
	return keys, nil
}

func rsaKey(key appleKey) (*rsa.PublicKey, error) {
	decode := func(value string) ([]byte, error) {
		return base64.RawURLEncoding.DecodeString(value)
	}
	n, err := decode(key.N)
	if err != nil {
		return nil, err
	}
	e, err := decode(key.E)
	if err != nil {
		return nil, err
	}
	var exponent int
	for _, value := range e {
		exponent = exponent<<8 | int(value)
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exponent}, nil
}

func (s *Service) findOrCreateAppleUser(ctx context.Context, subject, email, fullName string) (users.User, error) {
	subject = strings.TrimSpace(subject)
	email = strings.ToLower(strings.TrimSpace(email))
	fullName = strings.TrimSpace(fullName)
	if subject == "" {
		return users.User{}, apperr.ErrUnauthenticated
	}
	if row, err := s.sessions.q.GetAuthIdentity(ctx, authdb.GetAuthIdentityParams{Provider: "apple", ProviderSubject: subject}); err == nil {
		return s.users.ByID(ctx, row.UserID)
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return users.User{}, apperr.Wrap(err, "lookup apple identity")
	}
	if email == "" {
		return users.User{}, apperr.ErrUnauthenticated
	}
	if fullName == "" {
		fullName = "North user"
	}
	user, err := s.users.ByEmail(ctx, email)
	if err != nil && !apperr.Is(err, apperr.ErrNotFound) {
		return users.User{}, err
	}
	if apperr.Is(err, apperr.ErrNotFound) {
		user, err = s.users.Register(ctx, users.Registration{Email: email, DisplayName: fullName, Timezone: "UTC", Locale: users.ResolveLocale(i18n.LocaleFrom(ctx))})
		if err != nil {
			return users.User{}, err
		}
	}
	storedEmail := email
	_, err = s.sessions.q.CreateAuthIdentity(ctx, authdb.CreateAuthIdentityParams{UserID: user.ID, Provider: "apple", ProviderSubject: subject, Email: &storedEmail})
	if err != nil {
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
			return users.User{}, apperr.Wrap(err, "link apple identity")
		}
	}
	return user, nil
}

// splitList reads a comma-separated setting, dropping blanks.
func splitList(value string) []string {
	var out []string
	for part := range strings.SplitSeq(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
