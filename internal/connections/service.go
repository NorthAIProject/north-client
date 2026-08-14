package connections

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// tokenPrefix marks a North agent token.
//
// Worth the three characters: it makes the credential recognisable in a config
// file somebody pastes into a support conversation, and greppable by anyone
// auditing a machine for secrets that should not be there.
const tokenPrefix = "nk_"

// tokenBytes is the entropy behind each token. 32 bytes is the same order as a
// session token, and this credential is longer-lived than a session.
const tokenBytes = 32

// displayPrefixLen is how much of the token the settings page may show: the
// marker plus eight characters. Enough to tell two connections apart, far too
// little to shorten a search for the other 248 bits.
const displayPrefixLen = len(tokenPrefix) + 8

// maxNameLen bounds the user's label for a connection. A name is read in a
// list, not stored for its content.
const maxNameLen = 60

// UserLoader is the slice of users.Service this package needs. Named as an
// interface so authentication does not drag the whole user service into
// anything that only wants to check a token.
type UserLoader interface {
	ByID(ctx context.Context, id uuid.UUID) (users.User, error)
}

type Service struct {
	repo  *Repository
	users UserLoader

	// baseURL is the address an outside agent reaches North on, from
	// configuration rather than from the request. A URL derived from r.Host is
	// attacker-controlled, and these instructions carry a live credential — the
	// one combination that must never be pointed somewhere else.
	baseURL string
}

func NewService(repo *Repository, users UserLoader, baseURL string) *Service {
	return &Service{repo: repo, users: users, baseURL: strings.TrimRight(baseURL, "/")}
}

// Issue creates a connection and returns its token, once.
//
// The token is generated here, hashed, and handed back to the caller; the hash
// is what is stored. There is deliberately no way to read it again, so the
// recovery path for a lost token is to revoke and issue another.
func (s *Service) Issue(ctx context.Context, userID uuid.UUID, name string, kind ClientKind) (Issued, error) {
	name = strings.TrimSpace(name)

	var fieldErrs apperr.FieldErrors
	if name == "" {
		fieldErrs = fieldErrs.Add("name", "Give this connection a name, so you know which one to revoke later.")
	} else if utf8.RuneCountInString(name) > maxNameLen {
		fieldErrs = fieldErrs.Add("name", "That name is too long.")
	}
	if !kind.valid() {
		fieldErrs = fieldErrs.Add("client_kind", "Choose which client this is for.")
	}
	if err := fieldErrs.OrNil(); err != nil {
		return Issued{}, err
	}

	token, err := newToken()
	if err != nil {
		return Issued{}, err
	}

	sum := sha256.Sum256([]byte(token))
	conn, err := s.repo.Insert(ctx, userID, name, kind, sum[:], token[:displayPrefixLen])
	if err != nil {
		return Issued{}, err
	}

	return Issued{Connection: conn, Token: token}, nil
}

func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]Connection, error) {
	return s.repo.List(ctx, userID)
}

// Get returns one of the user's live connections.
//
// Filtered from the list rather than fetched by id: the list is already scoped
// to the owner and already excludes revoked rows, so this cannot return
// somebody else's connection or a dead one by construction. A person has a
// handful of these, and a second query to save reading four rows would be
// paying in complexity for nothing.
func (s *Service) Get(ctx context.Context, id, userID uuid.UUID) (Connection, error) {
	list, err := s.repo.List(ctx, userID)
	if err != nil {
		return Connection{}, err
	}
	for _, conn := range list {
		if conn.ID == id {
			return conn, nil
		}
	}
	return Connection{}, apperr.ErrNotFound
}

func (s *Service) Revoke(ctx context.Context, id, userID uuid.UUID) error {
	return s.repo.Revoke(ctx, id, userID)
}

// Authenticate resolves a presented bearer token to the account it acts as.
//
// It satisfies mcpserver.Authenticator. Every failure returns
// ErrUnauthenticated and nothing else: an unknown token and a revoked one are
// indistinguishable to the caller, because telling them apart would confirm
// that a guessed token once existed.
func (s *Service) Authenticate(ctx context.Context, token string) (users.User, error) {
	if !strings.HasPrefix(token, tokenPrefix) {
		return users.User{}, apperr.ErrUnauthenticated
	}

	// The lookup is by SHA-256 of the whole token, so the comparison happens
	// inside the index on a value an attacker cannot steer. There is no
	// timing-safe compare here because there is no secret-dependent branch:
	// the hash either matches a row or it does not.
	sum := sha256.Sum256([]byte(token))
	conn, err := s.repo.ByTokenHash(ctx, sum[:])
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			return users.User{}, apperr.ErrUnauthenticated
		}
		return users.User{}, err
	}

	user, err := s.users.ByID(ctx, conn.UserID)
	if err != nil {
		return users.User{}, apperr.Wrap(err, "load user for agent connection")
	}

	// Best-effort: a failed touch must not fail the request it is describing.
	// The consequence of losing one is a "last used" that lags, which is not
	// worth refusing an otherwise valid call over.
	_ = s.repo.Touch(ctx, conn.ID)

	return user, nil
}

// newToken returns a token with tokenBytes of entropy, URL-safe and unpadded
// so it survives a JSON config, a shell command, and a TOML header untouched.
func newToken() (string, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", apperr.Wrap(err, "generate agent token")
	}
	return tokenPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}
