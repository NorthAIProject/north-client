// Package toolaudit records what North has done to somebody's data on their
// behalf.
//
// The coach's tool calls already survive in messages.tool_calls, but that only
// answers for the coach. An external MCP client calls the same capabilities and
// never touches a conversation, so its writes — the ones with no confirmation
// step in front of them — would otherwise be the least visible in the system.
// This package is the one place both surfaces are answerable from.
package toolaudit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Surface names where a call came from.
//
// Carried on the request context rather than fixed when the registry is built:
// one registry instance serves both the coach and MCP, so it cannot know from
// its own construction which one is calling.
type Surface string

const (
	SurfaceCoach Surface = "coach"
	SurfaceMCP   Surface = "mcp"

	// SurfaceUnknown is what an unlabelled call records as. Recording the
	// uncertainty is better than guessing a surface and being wrong in a log
	// somebody may rely on later.
	SurfaceUnknown Surface = "unknown"
)

// Outcome is what happened.
type Outcome string

const (
	OutcomeExecuted Outcome = "executed"
	OutcomeFailed   Outcome = "failed"

	// OutcomeDeclined is recorded when a person refuses a write at the
	// confirmation card. It never reaches the code path the other two do — the
	// tool is not invoked at all — so it is written separately, and it earns a
	// row because a refusal is a decision worth being able to point at.
	OutcomeDeclined Outcome = "declined"
)

// Execution is one thing North did, or was asked to do and did not.
type Execution struct {
	ID     uuid.UUID
	UserID uuid.UUID

	Tool string

	// Arguments as they were sent. An audit line without them — "logged a
	// check-in" — does not answer the question this exists for.
	Arguments json.RawMessage

	Surface Surface
	Outcome Outcome

	// Detail is what the tool said, or why it failed. Read by a person.
	Detail string

	CreatedAt time.Time
}

// Executed reports whether this call actually changed anything.
func (e Execution) Executed() bool { return e.Outcome == OutcomeExecuted }

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service { return &Service{repo: repo} }

// Record stores one execution.
func (s *Service) Record(ctx context.Context, e Execution) error {
	if e.Surface == "" {
		e.Surface = SurfaceUnknown
	}
	return s.repo.Insert(ctx, e)
}

// List returns one person's executions, newest first.
func (s *Service) List(ctx context.Context, userID uuid.UUID, limit int) ([]Execution, error) {
	if limit <= 0 {
		limit = defaultLimit
	}
	return s.repo.List(ctx, userID, limit)
}

// defaultLimit bounds the page when a caller names no limit. Enough to cover a
// long session's worth of activity without reading a year into memory.
const defaultLimit = 100
