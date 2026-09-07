package mcpauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/connections"
	"github.com/NorthAIProject/north-client/internal/mcpserver"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// maxClients is a hard ceiling on registrations.
//
// Registration is open, because a gate in the middle of the flow would undo
// the one thing it exists for. Open needs a bound, and this one is deliberately
// a loud failure rather than a silent eviction: an operator who sees it knows
// the endpoint is being farmed.
const maxClients = 50_000

// Registration bounds. A client names itself and lists its own callbacks, so
// all of it is attacker-chosen and all of it is bounded.
const (
	maxClientNameLen   = 120
	maxRedirectURIs    = 5
	maxRedirectURILen  = 512
	maxSoftwareIDLen   = 120
	clientIDPrefix     = "mcpc_"
	clientIDBytes      = 32
	codeBytes          = 32
	refreshTokenBytes  = 32
	requestVerifierLen = 32
)

// Grants is the slice of internal/connections this package needs.
//
// An interface so the flow can be tested without a database, and so the
// dependency reads as "this package issues grants through that one" rather
// than as free access to everything connections can do.
type Grants interface {
	IssueGrant(ctx context.Context, in connections.GrantInput) (connections.Issued, error)
	RotateGrantToken(ctx context.Context, connectionID uuid.UUID, ttl time.Duration) (string, error)
	Revoke(ctx context.Context, id, userID uuid.UUID) error
}

type Service struct {
	repo   *Repository
	grants Grants

	// baseURL is this deployment's own origin. It is what the RFC 8707
	// audience is built from, so a token issued here cannot be replayed
	// against another MCP server the same client talks to.
	baseURL string

	now func() time.Time
}

func NewService(repo *Repository, grants Grants, baseURL string) *Service {
	return &Service{
		repo:    repo,
		grants:  grants,
		baseURL: strings.TrimSuffix(baseURL, "/"),
		now:     time.Now,
	}
}

// WithClock fixes now, for tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// Resource is the RFC 8707 audience every token here is bound to.
func (s *Service) Resource() string { return s.baseURL + "/mcp" }

// --- registration ----------------------------------------------------------

// RegisterClient stores a self-registered client under RFC 7591.
//
// Open, as the MCP specification intends, and bounded six ways: a rate limit
// in front of the handler, public clients only, the field bounds above, a
// dedupe key, a sweep of registrations that never produced a grant, and the
// hard ceiling.
func (s *Service) RegisterClient(ctx context.Context, in Registration) (Client, error) {
	// Public clients only. North issues no client secrets, so there is never
	// one to leak, and a client claiming it can keep one is claiming something
	// this server will not honour.
	if method := strings.TrimSpace(in.TokenEndpointAuthMethod); method != "" && method != "none" {
		return Client{}, apperr.Wrap(apperr.ErrValidation,
			"token_endpoint_auth_method must be none: this server issues no client secrets")
	}

	name := strings.TrimSpace(in.ClientName)
	if name == "" {
		return Client{}, apperr.Wrap(apperr.ErrValidation, "client_name is required")
	}
	if len(name) > maxClientNameLen {
		return Client{}, apperr.Wrap(apperr.ErrValidation, "client_name is too long")
	}
	if len(in.SoftwareID) > maxSoftwareIDLen {
		return Client{}, apperr.Wrap(apperr.ErrValidation, "software_id is too long")
	}

	if len(in.RedirectURIs) == 0 {
		return Client{}, apperr.Wrap(apperr.ErrValidation, "at least one redirect_uri is required")
	}
	if len(in.RedirectURIs) > maxRedirectURIs {
		return Client{}, apperr.Wrap(apperr.ErrValidation, "too many redirect_uris")
	}
	for _, uri := range in.RedirectURIs {
		if len(uri) > maxRedirectURILen {
			return Client{}, apperr.Wrap(apperr.ErrValidation, "a redirect_uri is too long")
		}
		if err := ValidateRedirectURI(uri); err != nil {
			return Client{}, err
		}
	}

	grantTypes := in.GrantTypes
	if len(grantTypes) == 0 {
		grantTypes = []string{"authorization_code", "refresh_token"}
	}
	for _, grant := range grantTypes {
		if grant != "authorization_code" && grant != "refresh_token" {
			return Client{}, apperr.Wrap(apperr.ErrValidation,
				"only the authorization_code and refresh_token grants are supported")
		}
	}

	// An identical registration returns the existing row. A web client with a
	// stable callback therefore registers once however many times it asks;
	// a native client whose port changes every launch does not dedupe, which
	// is what the sweep is for.
	key := dedupeKey(name, in.SoftwareID, in.RedirectURIs)
	if existing, err := s.repo.ClientByDedupeKey(ctx, key); err == nil {
		return existing, nil
	} else if !apperr.Is(err, apperr.ErrNotFound) {
		return Client{}, err
	}

	count, err := s.repo.CountClients(ctx)
	if err != nil {
		return Client{}, err
	}
	if count >= maxClients {
		return Client{}, apperr.Wrap(apperr.ErrUnavailable,
			"this server is not accepting new client registrations")
	}

	id, err := randomString(clientIDPrefix, clientIDBytes)
	if err != nil {
		return Client{}, err
	}

	return s.repo.InsertClient(ctx, Client{
		ID:           id,
		Name:         name,
		RedirectURIs: in.RedirectURIs,
		GrantTypes:   grantTypes,
		SoftwareID:   in.SoftwareID,
	}, key)
}

// --- authorization ---------------------------------------------------------

// FatalAuthorizeError is a failure that must not be redirected anywhere.
//
// The distinction matters more than it looks. Once a redirect_uri has been
// matched against a registration it is safe to send errors to, and doing so is
// what lets a client show a useful message. Before that it is an unvalidated
// string from a query parameter, and redirecting to it is the classic
// open-redirect-via-OAuth bug — so an unknown client or an unregistered
// callback renders a page here instead.
type FatalAuthorizeError struct{ Reason string }

func (e FatalAuthorizeError) Error() string { return e.Reason }

// RedirectableError is a failure the client should be told about, at its own
// validated callback.
type RedirectableError struct {
	Code        string
	Description string

	// RedirectURI is the validated destination, and State is echoed back so
	// the client can match the response to the request it made.
	RedirectURI string
	State       string
}

func (e RedirectableError) Error() string { return e.Code + ": " + e.Description }

// URL is where to send the browser.
func (e RedirectableError) URL() string {
	return appendQuery(e.RedirectURI, map[string]string{
		"error":             e.Code,
		"error_description": e.Description,
		"state":             e.State,
	})
}

// BeginAuthorization validates an authorization request and parks it.
//
// The returned verifier is the value for the north_oauth_req cookie. It is
// what binds the parked request to this browser: without it, a guessed request
// id would let somebody have a victim approve a request they started.
func (s *Service) BeginAuthorization(ctx context.Context, in AuthorizeParams) (Request, string, error) {
	// Fatal, in order. Neither an unknown client nor an unregistered callback
	// gives us anywhere safe to redirect to.
	if strings.TrimSpace(in.ClientID) == "" {
		return Request{}, "", FatalAuthorizeError{Reason: "This request did not say which application is asking."}
	}

	client, err := s.repo.Client(ctx, in.ClientID)
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			return Request{}, "", FatalAuthorizeError{
				Reason: "The application making this request is not registered with Khepri.",
			}
		}
		return Request{}, "", err
	}

	redirectURI, err := MatchRedirectURI(client.RedirectURIs, in.RedirectURI)
	if err != nil {
		return Request{}, "", FatalAuthorizeError{
			Reason: "This request asked to send your access somewhere the application has not registered.",
		}
	}

	// From here the callback is validated, so a failure can be reported to it.
	fail := func(code, description string) (Request, string, error) {
		return Request{}, "", RedirectableError{
			Code:        code,
			Description: description,
			RedirectURI: redirectURI,
			State:       in.State,
		}
	}

	if in.ResponseType != "code" {
		return fail("unsupported_response_type", "only the code response type is supported")
	}
	if challengeErr := ValidateChallenge(in.CodeChallenge, in.CodeChallengeMethod); challengeErr != nil {
		return fail("invalid_request", challengeErr.Error())
	}

	// RFC 8707. Absent defaults to this deployment, because not every client
	// sends it; present and different is refused rather than quietly retargeted.
	resource := strings.TrimSpace(in.Resource)
	if resource == "" {
		resource = s.Resource()
	}
	if resource != s.Resource() {
		return fail("invalid_target", "this authorization server does not issue tokens for that resource")
	}

	scope, err := normaliseScope(in.Scope)
	if err != nil {
		return fail("invalid_scope", err.Error())
	}

	verifier, err := randomString("", requestVerifierLen)
	if err != nil {
		return Request{}, "", err
	}

	parked, err := s.repo.InsertRequest(ctx, Request{
		ClientID:    client.ID,
		RedirectURI: redirectURI,
		State:       in.State,
		Scope:       scope,
		Resource:    resource,
		ExpiresAt:   s.now().Add(RequestTTL),
	}, in.CodeChallenge, in.CodeChallengeMethod, hash(verifier))
	if err != nil {
		return Request{}, "", err
	}

	parked.ClientName = client.Name
	return parked, verifier, nil
}

// LoadRequest reads a parked request, checking the browser's cookie.
func (s *Service) LoadRequest(ctx context.Context, id uuid.UUID, verifier string) (Request, error) {
	stored, err := s.repo.Request(ctx, id, hash(verifier))
	if err != nil {
		return Request{}, err
	}

	client, err := s.repo.Client(ctx, stored.ClientID)
	if err != nil {
		return Request{}, err
	}
	stored.ClientName = client.Name
	return stored.Request, nil
}

// MarkAccountCreated records that the account was made on the consent screen.
func (s *Service) MarkAccountCreated(ctx context.Context, id uuid.UUID, verifier string) error {
	return s.repo.MarkRequestAccountCreated(ctx, id, hash(verifier))
}

// Approve mints an authorization code and returns where to send the browser.
//
// scope may narrow what was asked for — the consent screen offers a read-only
// toggle — but never widen it.
func (s *Service) Approve(ctx context.Context, id uuid.UUID, verifier string, userID uuid.UUID, scope string) (string, error) {
	stored, err := s.repo.Request(ctx, id, hash(verifier))
	if err != nil {
		return "", err
	}

	granted, err := narrowScope(stored.Scope, scope)
	if err != nil {
		return "", err
	}

	// Consumed before the code is minted, and the row count is what says we
	// won: two Approve posts from a double-clicked button must not produce two
	// codes for one consent.
	if consumeErr := s.repo.ConsumeRequest(ctx, id, hash(verifier)); consumeErr != nil {
		return "", consumeErr
	}

	code, err := randomString("", codeBytes)
	if err != nil {
		return "", err
	}

	if err := s.repo.InsertCode(ctx, storedCode{
		Hash:                hash(code),
		ClientID:            stored.ClientID,
		UserID:              userID,
		RedirectURI:         stored.RedirectURI,
		CodeChallenge:       stored.CodeChallenge,
		CodeChallengeMethod: stored.CodeChallengeMethod,
		Scope:               granted,
		Resource:            stored.Resource,
		ExpiresAt:           s.now().Add(CodeTTL),
	}); err != nil {
		return "", err
	}

	return appendQuery(stored.RedirectURI, map[string]string{
		"code":  code,
		"state": stored.State,
	}), nil
}

// Deny closes the request and reports the refusal to the client.
func (s *Service) Deny(ctx context.Context, id uuid.UUID, verifier string) (string, error) {
	stored, err := s.repo.Request(ctx, id, hash(verifier))
	if err != nil {
		return "", err
	}
	if err := s.repo.ConsumeRequest(ctx, id, hash(verifier)); err != nil {
		return "", err
	}

	return appendQuery(stored.RedirectURI, map[string]string{
		"error":             "access_denied",
		"error_description": "the person refused this request",
		"state":             stored.State,
	}), nil
}

// --- tokens ----------------------------------------------------------------

// ExchangeCode turns an authorization code into a grant and its first tokens.
func (s *Service) ExchangeCode(ctx context.Context, in CodeExchange) (Tokens, error) {
	stored, err := s.repo.Code(ctx, hash(in.Code))
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			return Tokens{}, invalidGrant("the authorization code is not valid")
		}
		return Tokens{}, err
	}

	// A code presented twice is a replay, and RFC 6749 section 10.5 says to
	// revoke everything it produced: whoever holds the second copy may hold
	// the first, so the grant is not trustworthy any more.
	if stored.Consumed != nil {
		s.revokeGrant(ctx, stored)
		return Tokens{}, invalidGrant("the authorization code has already been used")
	}
	if stored.ExpiresAt.Before(s.now()) {
		return Tokens{}, invalidGrant("the authorization code has expired")
	}

	// A code issued to one client cannot be redeemed by another, and the
	// redirect_uri has to match the one the code was issued against — both are
	// RFC 6749 section 4.1.3.
	if in.ClientID != stored.ClientID {
		return Tokens{}, invalidGrant("this authorization code was issued to a different client")
	}
	if in.RedirectURI != stored.RedirectURI {
		return Tokens{}, invalidGrant("redirect_uri does not match the authorization request")
	}
	if resource := strings.TrimSpace(in.Resource); resource != "" && resource != stored.Resource {
		return Tokens{}, apperr.Wrap(apperr.ErrValidation, "invalid_target")
	}

	if verifyErr := VerifyChallenge(stored.CodeChallenge, stored.CodeChallengeMethod, in.CodeVerifier); verifyErr != nil {
		return Tokens{}, invalidGrant("the code_verifier does not match")
	}

	client, err := s.repo.Client(ctx, stored.ClientID)
	if err != nil {
		return Tokens{}, err
	}

	issued, err := s.grants.IssueGrant(ctx, connections.GrantInput{
		UserID:        stored.UserID,
		ClientName:    client.Name,
		Scopes:        stored.Scope,
		Resource:      stored.Resource,
		OAuthClientID: client.ID,
		TTL:           AccessTokenTTL,
	})
	if err != nil {
		return Tokens{}, err
	}

	// After the grant exists, so the code row can point at what to revoke if
	// this code is ever presented again.
	if consumeErr := s.repo.ConsumeCode(ctx, stored.Hash, issued.ID); consumeErr != nil {
		return Tokens{}, consumeErr
	}
	_ = s.repo.TouchClient(ctx, client.ID)

	refresh, _, err := s.issueRefreshToken(ctx, issued.ID)
	if err != nil {
		return Tokens{}, err
	}

	return Tokens{
		AccessToken:  issued.Token,
		RefreshToken: refresh,
		Scope:        stored.Scope,
		ExpiresIn:    int(AccessTokenTTL.Seconds()),
	}, nil
}

// Refresh rotates a refresh token and issues a new access token.
func (s *Service) Refresh(ctx context.Context, in RefreshExchange) (Tokens, error) {
	stored, err := s.repo.RefreshToken(ctx, hash(in.RefreshToken))
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			return Tokens{}, invalidGrant("the refresh token is not valid")
		}
		return Tokens{}, err
	}

	// Single use, and reuse revokes the grant for the same reason a replayed
	// code does: a second holder of a token that should have been spent once
	// means the credential has escaped.
	if stored.Consumed != nil {
		_ = s.repo.ConsumeAllRefreshTokens(ctx, stored.ConnectionID)
		return Tokens{}, invalidGrant("the refresh token has already been used")
	}
	if stored.ExpiresAt.Before(s.now()) {
		return Tokens{}, invalidGrant("the refresh token has expired")
	}

	// Claimed before anything is issued. Two refresh requests arriving together
	// must not both succeed, and the row count is the only thing that says
	// which one won — issuing first and consuming afterwards would leave a
	// live token behind for the loser.
	if claimErr := s.repo.ConsumeRefreshToken(ctx, hash(in.RefreshToken), uuid.Nil); claimErr != nil {
		if apperr.Is(claimErr, apperr.ErrNotFound) {
			return Tokens{}, invalidGrant("the refresh token is not valid")
		}
		return Tokens{}, claimErr
	}

	access, err := s.grants.RotateGrantToken(ctx, stored.ConnectionID, AccessTokenTTL)
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			// Revoked between the last refresh and this one.
			return Tokens{}, invalidGrant("this connection has been revoked")
		}
		return Tokens{}, err
	}

	refresh, refreshID, err := s.issueRefreshToken(ctx, stored.ConnectionID)
	if err != nil {
		return Tokens{}, err
	}

	// Forensics only: which token superseded which. A failure here has already
	// been survived by a successful refresh.
	_ = s.repo.LinkReplaced(ctx, hash(in.RefreshToken), refreshID)

	return Tokens{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int(AccessTokenTTL.Seconds()),
	}, nil
}

// Sweep removes what has aged out. Called from the worker.
func (s *Service) Sweep(ctx context.Context) (Swept, error) {
	return s.repo.Sweep(ctx)
}

func (s *Service) issueRefreshToken(ctx context.Context, connectionID uuid.UUID) (string, uuid.UUID, error) {
	token, err := randomString("", refreshTokenBytes)
	if err != nil {
		return "", uuid.Nil, err
	}
	id, err := s.repo.InsertRefreshToken(ctx, connectionID, hash(token), s.now().Add(RefreshTokenTTL))
	if err != nil {
		return "", uuid.Nil, err
	}
	return token, id, nil
}

// revokeGrant turns off whatever a replayed code produced. Best effort: the
// caller is already returning an error, and failing to revoke must not turn
// a refusal into a 500 that says nothing useful.
func (s *Service) revokeGrant(ctx context.Context, code storedCode) {
	if code.ConnectionID == nil {
		return
	}
	_ = s.repo.ConsumeAllRefreshTokens(ctx, *code.ConnectionID)
	_ = s.grants.Revoke(ctx, *code.ConnectionID, code.UserID)
}

// --- helpers ---------------------------------------------------------------

// invalidGrant is the RFC 6749 error every token-endpoint refusal returns.
//
// One error code for every reason, deliberately: unknown, expired, replayed
// and wrong-client are indistinguishable to the caller, the same property the
// 401 on /mcp holds.
func invalidGrant(description string) error {
	return apperr.Wrap(apperr.ErrUnauthenticated, "%s", description)
}

// normaliseScope validates what a client asked for, defaulting to full access.
func normaliseScope(requested string) (string, error) {
	fields := strings.Fields(requested)
	if len(fields) == 0 {
		// Every token issued before scopes existed is full access, and a
		// connector that cannot log a check-in is not the product.
		return mcpserver.ScopeReadWrite, nil
	}
	if len(fields) > 1 {
		return "", fmt.Errorf("request one scope: %s or %s", mcpserver.ScopeRead, mcpserver.ScopeReadWrite)
	}
	switch fields[0] {
	case mcpserver.ScopeRead, mcpserver.ScopeReadWrite:
		return fields[0], nil
	default:
		return "", fmt.Errorf("unknown scope %q", fields[0])
	}
}

// narrowScope applies the consent screen's read-only toggle.
//
// It may only reduce. A person who chose read-only gets read-only; a request
// that arrived as read-only cannot be widened into read-write by anything the
// screen posts back.
func narrowScope(requested, chosen string) (string, error) {
	if chosen == "" || chosen == requested {
		return requested, nil
	}
	if chosen == mcpserver.ScopeRead {
		return mcpserver.ScopeRead, nil
	}
	return "", apperr.Wrap(apperr.ErrValidation, "a consent cannot grant more than was requested")
}

// dedupeKey identifies an identical registration.
//
// The redirect URIs are sorted so the same set in a different order is the
// same registration, and the parts are length-prefixed so that a name ending
// in a separator cannot be made to look like a different field.
func dedupeKey(name, softwareID string, redirectURIs []string) []byte {
	uris := make([]string, len(redirectURIs))
	copy(uris, redirectURIs)
	sort.Strings(uris)

	var b strings.Builder
	write := func(part string) {
		fmt.Fprintf(&b, "%d:%s|", len(part), part)
	}
	write(name)
	write(softwareID)
	for _, uri := range uris {
		write(uri)
	}

	sum := sha256.Sum256([]byte(b.String()))
	return sum[:]
}

// hash is how every credential this package issues is stored.
//
// Never sealed. internal/shared/secret exists so North can use a credential
// later, which is why BYOK needs it; a token North issued is only ever
// verified, so a hash cannot be replayed out of a database dump and there is
// no key to rotate.
func hash(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}

func randomString(prefix string, size int) (string, error) {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err != nil {
		return "", apperr.Wrap(err, "generate a random value")
	}
	return prefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

// appendQuery adds parameters to a validated redirect URI, keeping whatever
// query it already carried.
func appendQuery(redirectURI string, params map[string]string) string {
	parsed, err := url.Parse(redirectURI)
	if err != nil {
		// Unreachable: this URI was validated before it was stored.
		return redirectURI
	}

	query := parsed.Query()
	for key, value := range params {
		if value != "" {
			query.Set(key, value)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}
