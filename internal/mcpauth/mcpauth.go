// Package mcpauth is the authorization server in front of the /mcp endpoint.
//
// North is two things in this flow, and keeping them apart is what makes the
// code readable. internal/mcpserver is the *resource server*: it holds the
// tools and checks bearer tokens. This package is the *authorization server*:
// it registers clients, runs the consent screen, and issues the tokens the
// resource server later verifies. They share a table and nothing else.
//
// The point of it is one pasted URL. Before this, connecting an agent meant
// generating a token on the settings page and editing a JSON file, and
// docs/mcp-oauth-plan.md named the consequence: somebody who has never opened
// .mcp.json stops there. With OAuth the person pastes https://.../mcp into
// their client, approves a screen, and never sees a token — and because the
// consent screen can create the account, that same URL is also the signup.
//
// Two design decisions are worth knowing before reading further.
//
// The authorization request is *parked in the database* rather than carried in
// the query string. It has to survive a signup, a sign-in, or a round trip
// through Google or a passkey, and carrying only an opaque id through those
// hops means none of the OAuth parameters can be edited in between.
//
// Token minting is *not here*. An access token is presented to /mcp exactly
// like one pasted from the settings page, so internal/connections mints and
// stores it; this package decides who gets one.
package mcpauth

import (
	"time"

	"github.com/google/uuid"
)

// Lifetimes.
//
// An hour bounds what a leaked access token is worth. Thirty days of refresh
// means a laptop closed for a fortnight still works without opening a browser,
// and an abandoned connection dies on its own — which is also the sweep.
// Sixty seconds for a code is the round trip from the browser redirect to the
// client's token request and nothing more.
const (
	AccessTokenTTL  = time.Hour
	RefreshTokenTTL = 30 * 24 * time.Hour
	CodeTTL         = 60 * time.Second

	// RequestTTL is how long a parked authorization request stays usable. Long
	// enough to read the consent screen, create an account and choose a
	// password; short enough that an abandoned one is not lying around.
	RequestTTL = 10 * time.Minute
)

// Client is a registration made under RFC 7591.
type Client struct {
	ID   string
	Name string

	RedirectURIs []string
	GrantTypes   []string
	SoftwareID   string

	CreatedAt time.Time
}

// Registration is a client's self-description, as posted to /oauth/register.
//
// Every field is chosen by whoever is registering. None of it is trusted: the
// name is bounded and escaped before it reaches the consent screen, and the
// redirect URIs are validated before they are stored.
type Registration struct {
	ClientName   string
	RedirectURIs []string
	GrantTypes   []string
	SoftwareID   string

	// TokenEndpointAuthMethod must be "none" or absent. North issues no client
	// secrets: every MCP client is a public client, and a secret it could not
	// keep would be a secret to leak.
	TokenEndpointAuthMethod string
}

// AuthorizeParams is an authorization request as it arrives on the query
// string.
type AuthorizeParams struct {
	ClientID     string
	RedirectURI  string
	ResponseType string
	State        string
	Scope        string

	CodeChallenge       string
	CodeChallengeMethod string

	// Resource is the RFC 8707 audience. Absent is allowed and defaults to
	// this deployment's own /mcp, because not every client sends it.
	Resource string
}

// Request is a parked authorization request, with the client resolved.
type Request struct {
	ID uuid.UUID

	ClientID   string
	ClientName string

	// RedirectURI is the validated destination. Anything that reaches this
	// field has already been matched against the client's registration, so it
	// is safe to redirect to — which the unvalidated input never is.
	RedirectURI string

	State    string
	Scope    string
	Resource string

	// AccountCreated records that the account was made on the consent screen
	// itself. It is the number that decides whether this whole flow acquires
	// anybody or merely convenienced people who had already signed up.
	AccountCreated bool

	ExpiresAt time.Time
}

// RedirectHost is the host the code will be sent to, for the consent screen.
//
// The screen shows it verbatim beside the client's chosen name, because the
// name is attacker-controlled and the host is not: a registration calling
// itself "Khepri Official Sync" cannot also claim to be sending the grant to
// localhost.
func (r Request) RedirectHost() string {
	return redirectHost(r.RedirectURI)
}

// Tokens is a successful token response.
type Tokens struct {
	AccessToken  string
	RefreshToken string
	Scope        string

	// ExpiresIn is seconds, as RFC 6749 writes it.
	ExpiresIn int
}

// CodeExchange is an authorization_code grant request.
type CodeExchange struct {
	Code         string
	ClientID     string
	RedirectURI  string
	CodeVerifier string
	Resource     string
}

// RefreshExchange is a refresh_token grant request.
type RefreshExchange struct {
	RefreshToken string
	ClientID     string

	// Scope, when present, may only narrow what the grant already holds. A
	// client cannot widen its own access by asking for more on a refresh.
	Scope string
}
