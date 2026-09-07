// Package connections owns the personal access tokens that let a user point
// their own agent — Claude Code, Codex, Hermes, any MCP client — at their own
// North account.
//
// The single MCP_API_TOKEN it replaces maps one static bearer to one hardcoded
// account, which is why that endpoint belongs on a tailnet. A token here
// carries the identity with it, so /mcp can be served publicly without any
// caller being able to act as someone else.
//
// The plaintext token exists for exactly one moment: the response to the
// request that created it. Nothing in this package can return it afterwards,
// because nothing in this package has it.
package connections

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// ClientKind is the agent the setup instructions were written for.
//
// Presentation only. Nothing about authentication varies by kind, and a token
// issued for one client works in any of them — the kind decides which config
// snippet the settings page shows, nothing more.
type ClientKind string

const (
	ClientClaudeCode ClientKind = "claude_code"
	ClientCodex      ClientKind = "codex"
	ClientHermes     ClientKind = "hermes"
	ClientOther      ClientKind = "other"
)

// ClientKindFor maps a self-registered OAuth client's name onto the existing
// vocabulary.
//
// A dynamically registered client names itself, so there is no dropdown to
// read the kind from. Mapping onto the four kinds that already exist — rather
// than adding a fifth for "arrived over OAuth" — is what lets Label and every
// branch on the settings page keep working with no new case. How the
// credential was issued is a separate question, answered by Issuance.
func ClientKindFor(clientName string) ClientKind {
	name := strings.ToLower(clientName)
	switch {
	case strings.Contains(name, "claude code"), strings.Contains(name, "claude-code"):
		return ClientClaudeCode
	case strings.Contains(name, "codex"):
		return ClientCodex
	case strings.Contains(name, "hermes"):
		return ClientHermes
	default:
		// Claude Desktop and claude.ai land here, deliberately. They are real
		// clients with no config snippet to show, which is exactly what
		// ClientOther means.
		return ClientOther
	}
}

// ClientKinds is the list the settings page offers, in the order it offers it.
var ClientKinds = []ClientKind{ClientClaudeCode, ClientCodex, ClientHermes, ClientOther}

// Label is how the kind is written for a person.
func (k ClientKind) Label() string {
	switch k {
	case ClientClaudeCode:
		return "Claude Code"
	case ClientCodex:
		return "Codex"
	case ClientHermes:
		return "Hermes"
	default:
		return "Other MCP client"
	}
}

func (k ClientKind) valid() bool {
	switch k {
	case ClientClaudeCode, ClientCodex, ClientHermes, ClientOther:
		return true
	default:
		return false
	}
}

// Connection is a connected agent as the settings page sees it.
//
// There is no token field, and that is the point: this is the type that
// reaches a template, and a projection that cannot carry a secret cannot leak
// one. Compare strava.Status, which exists for the same reason.
type Connection struct {
	ID     uuid.UUID
	UserID uuid.UUID
	Name   string
	Kind   ClientKind

	// TokenPrefix is the first few characters of the token, enough to tell two
	// connections apart and far too little to guess the rest from.
	TokenPrefix string

	CreatedAt time.Time

	// LastUsedAt is nil until the token is first presented, which is how a
	// connection that was set up is told apart from one that was issued and
	// forgotten. Written lazily, so it lags real use by up to five minutes.
	LastUsedAt *time.Time

	// Scopes is what the token may do, as the string the MCP surface
	// enforces. Empty means full access, which is every token issued by hand.
	Scopes string

	// ExpiresAt is nil for a token issued by hand, which does not expire. An
	// OAuth access token expires within the hour and is replaced in place, so
	// a time in the past means "needs refreshing", not "gone".
	ExpiresAt *time.Time

	// Issuance is how the credential came to exist: pasted from the settings
	// page, or granted through the OAuth consent screen. Distinct from Kind,
	// which is which client the setup was written for.
	Issuance Issuance
}

// Issuance is how a connection's credential was issued.
type Issuance string

const (
	// IssuancePAT is a token generated on the settings page and pasted into a
	// configuration file by hand.
	IssuancePAT Issuance = "pat"

	// IssuanceOAuth is a grant approved on the consent screen, which the
	// person never sees the token for.
	IssuanceOAuth Issuance = "oauth"
)

// Expired reports whether the current access token has aged out. Only an
// OAuth grant can be expired, and an expired one is still a live connection:
// the next refresh replaces the token.
func (c Connection) Expired() bool {
	return c.ExpiresAt != nil && c.ExpiresAt.Before(time.Now())
}

// Used reports whether this connection has ever authenticated a request.
func (c Connection) Used() bool { return c.LastUsedAt != nil }

// GrantRow is what the repository needs to store an approved OAuth consent.
//
// A struct rather than nine positional arguments, because four of them are
// strings and swapping two would be a silent bug.
type GrantRow struct {
	UserID        uuid.UUID
	Name          string
	Kind          ClientKind
	TokenHash     []byte
	TokenPrefix   string
	Scopes        string
	ExpiresAt     time.Time
	Resource      string
	OAuthClientID string
}

// Issued is a newly created connection together with its plaintext token.
//
// Returned only by Service.Issue, and only once. The token is not stored, so a
// user who loses it revokes this connection and creates another.
type Issued struct {
	Connection
	Token string
}
