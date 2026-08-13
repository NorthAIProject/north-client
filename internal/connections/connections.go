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
}

// Used reports whether this connection has ever authenticated a request.
func (c Connection) Used() bool { return c.LastUsedAt != nil }

// Issued is a newly created connection together with its plaintext token.
//
// Returned only by Service.Issue, and only once. The token is not stored, so a
// user who loses it revokes this connection and creates another.
type Issued struct {
	Connection
	Token string
}
