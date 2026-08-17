// Package integrations owns North's outbound MCP connections: the external
// servers a person has connected, and the adapters that turn what those servers
// answer into plain summary strings for the coach.
//
// The other direction lives in internal/mcpserver, which is North serving tools
// to somebody else's agent. These two are opposites and share nothing.
package integrations

import (
	"time"

	"github.com/google/uuid"
)

// ProviderCalendar is the one integration that exists today.
//
// A string rather than an enum with one member, because the table's provider
// column has no CHECK constraint and a second provider should be a new adapter
// beside calendar.go, not a schema change.
const ProviderCalendar = "calendar"

const (
	StatusOK     = "ok"
	StatusFailed = "failed"
)

// Connection is one person's link to one external MCP server.
//
// The token is deliberately absent: it is sealed in the database and opened
// only inside the repository, on the path that is about to use it. Nothing that
// renders a page or logs a line can reach it from here.
type Connection struct {
	UserID   uuid.UUID
	Provider string
	Endpoint string

	Status        string
	LastError     string
	LastCheckedAt *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Connected reports whether this link is usable.
func (c Connection) Connected() bool { return c.Endpoint != "" }

// Healthy reports whether the last attempt to reach the server worked.
func (c Connection) Healthy() bool { return c.Status == StatusOK }

// Event is one entry from an external calendar.
//
// The smallest shape a coach needs: what, when, and how long. Anything richer
// would be North modelling somebody else's calendar, which is exactly the
// coupling the ticket's "coach only sees summary strings" rule forbids.
type Event struct {
	Title   string
	Start   time.Time
	AllDay  bool
	Summary string
}
