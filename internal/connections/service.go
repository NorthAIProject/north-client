package connections

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"time"
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
	user, _, err := s.AuthenticateScoped(ctx, token)
	return user, err
}

// AuthenticateScoped is Authenticate, and also reports what the token may do.
//
// It satisfies mcpserver.ScopedAuthenticator. The scope is returned verbatim
// as stored, including the empty string every token issued before scopes
// existed carries; what empty means is the MCP surface's decision, not this
// package's, and duplicating that judgement here would give it two homes.
func (s *Service) AuthenticateScoped(ctx context.Context, token string) (users.User, string, error) {
	if !strings.HasPrefix(token, tokenPrefix) {
		return users.User{}, "", apperr.ErrUnauthenticated
	}

	// The lookup is by SHA-256 of the whole token, so the comparison happens
	// inside the index on a value an attacker cannot steer. There is no
	// timing-safe compare here because there is no secret-dependent branch:
	// the hash either matches a row or it does not.
	sum := sha256.Sum256([]byte(token))
	conn, err := s.repo.ByTokenHash(ctx, sum[:])
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			return users.User{}, "", apperr.ErrUnauthenticated
		}
		return users.User{}, "", err
	}

	user, err := s.users.ByID(ctx, conn.UserID)
	if err != nil {
		return users.User{}, "", apperr.Wrap(err, "load user for agent connection")
	}

	// Best-effort: a failed touch must not fail the request it is describing.
	// The consequence of losing one is a "last used" that lags, which is not
	// worth refusing an otherwise valid call over.
	_ = s.repo.Touch(ctx, conn.ID)

	return user, conn.Scopes, nil
}

// ConnectorURL is the one value a person pastes into their agent.
//
// The same string the MCP endpoint is served at, which is also the string the
// OAuth discovery documents describe. One source for it, so the settings page
// cannot show a URL the discovery documents disagree with.
func (s *Service) ConnectorURL() string {
	return strings.TrimSuffix(s.baseURL, "/") + "/mcp"
}

// RevokeByToken turns off the connection a presented access token belongs to.
//
// For RFC 7009, where a client disconnecting hands back whichever token it
// holds. Returns ErrNotFound for a token that matches nothing, which the
// revocation endpoint deliberately reports as success: a different answer for
// an unknown token would confirm which tokens are real.
func (s *Service) RevokeByToken(ctx context.Context, token string) error {
	if !strings.HasPrefix(token, tokenPrefix) {
		return apperr.ErrNotFound
	}

	sum := sha256.Sum256([]byte(token))
	conn, err := s.repo.ByTokenHash(ctx, sum[:])
	if err != nil {
		return err
	}
	return s.repo.Revoke(ctx, conn.ID, conn.UserID)
}

// RevokeGrant turns off a connection by id, without a user to scope it to.
//
// For the OAuth paths that have already established the right to do it: the
// revocation endpoint, where the caller presented a token belonging to this
// row, and the replay path, where the id came from a code row. Anything
// driven by a form must use Revoke.
func (s *Service) RevokeGrant(ctx context.Context, connectionID uuid.UUID) error {
	return s.repo.RevokeGrant(ctx, connectionID)
}

// GrantInput describes a consent the person has just approved.
type GrantInput struct {
	UserID uuid.UUID

	// ClientName is what the client called itself at registration, and is
	// therefore chosen by whoever registered it. It becomes the connection's
	// name, which is what the settings page shows next to the revoke button.
	ClientName string

	Scopes   string
	Resource string

	// OAuthClientID ties the grant back to the registration, so the settings
	// page can say which client holds it and a code replay can find the grant
	// to revoke.
	OAuthClientID string

	// TTL is how long the access token is good for. Short, because a leaked
	// bearer token is only as dangerous as its remaining life.
	TTL time.Duration
}

// IssueGrant mints an access token for an approved consent and stores the
// connection.
//
// Token minting lives here rather than in internal/mcpauth so the nk_ prefix
// and the entropy have one home: an access token is presented to /mcp exactly
// like a pasted one, and AuthenticateScoped must accept it without knowing
// which flow produced it.
func (s *Service) IssueGrant(ctx context.Context, in GrantInput) (Issued, error) {
	token, err := newToken()
	if err != nil {
		return Issued{}, err
	}

	sum := sha256.Sum256([]byte(token))
	conn, err := s.repo.InsertGrant(ctx, GrantRow{
		UserID:      in.UserID,
		Name:        connectionName(in.ClientName),
		Kind:        ClientKindFor(in.ClientName),
		TokenHash:   sum[:],
		TokenPrefix: token[:displayPrefixLen],
		Scopes:      in.Scopes,
		ExpiresAt:   time.Now().Add(in.TTL),
		Resource:    in.Resource,

		OAuthClientID: in.OAuthClientID,
	})
	if err != nil {
		return Issued{}, err
	}
	return Issued{Connection: conn, Token: token}, nil
}

// RotateGrantToken issues a replacement access token for an existing grant.
//
// An update rather than a new row: one row is one grant, so the settings page
// shows one entry per connected agent however many hours it has been alive,
// and revoking that entry still kills everything the grant holds. token_prefix
// is deliberately not updated — it was only ever a label, and a stable one is
// worth more than one that tracks a token nobody can see.
func (s *Service) RotateGrantToken(ctx context.Context, connectionID uuid.UUID, ttl time.Duration) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256([]byte(token))
	if err := s.repo.RotateToken(ctx, connectionID, sum[:], time.Now().Add(ttl)); err != nil {
		return "", err
	}
	return token, nil
}

// connectionName keeps a client's self-chosen name to something a settings
// page can render on one line. The full name is not load-bearing anywhere;
// the redirect host is what identifies a client that matters.
func connectionName(clientName string) string {
	name := strings.TrimSpace(clientName)
	if name == "" {
		return "Connected agent"
	}
	if utf8.RuneCountInString(name) > maxNameLen {
		return strings.TrimSpace(string([]rune(name)[:maxNameLen]))
	}
	return name
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
