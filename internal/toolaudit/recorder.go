package toolaudit

import (
	"context"
	"log/slog"

	"github.com/NorthAIProject/north-client/internal/agent"
	"github.com/NorthAIProject/north-client/internal/coach"
)

// Recorder adapts this service to the two places executions happen.
//
// It lives here rather than in agent or coach because those packages must not
// depend on the slice that records them: agent defines the shape it reports and
// knows nothing about storage, and the same for coach. This is the one piece
// that knows about both, and it is wiring, not logic.
type Recorder struct {
	svc *Service
	log *slog.Logger
}

func NewRecorder(svc *Service) *Recorder {
	return &Recorder{svc: svc, log: slog.Default()}
}

// RecordToolRun stores a capability the registry ran, from either surface.
//
// Failures are logged and swallowed. A tool that worked must not be reported to
// the user as failed because the account of it could not be written, and the
// write has already happened by the time this is called — refusing here would
// change nothing except the reply.
func (r *Recorder) RecordToolRun(ctx context.Context, rec agent.Record) {
	outcome := OutcomeExecuted
	if rec.Failed {
		outcome = OutcomeFailed
	}

	if err := r.svc.Record(ctx, Execution{
		UserID:    rec.UserID,
		Tool:      rec.Tool,
		Arguments: rec.Arguments,
		Surface:   Surface(rec.Surface),
		Outcome:   outcome,
		Detail:    rec.Detail,
	}); err != nil {
		r.log.Error("could not record a tool execution",
			slog.String("tool", rec.Tool),
			slog.Any("error", err))
	}
}

// RecordDeclinedCall stores a write somebody refused at the confirmation card.
func (r *Recorder) RecordDeclinedCall(ctx context.Context, c coach.DeclinedCall) {
	if err := r.svc.Record(ctx, Execution{
		UserID:    c.UserID,
		Tool:      c.Tool,
		Arguments: c.Arguments,
		Surface:   SurfaceCoach,
		Outcome:   OutcomeDeclined,
		Detail:    "The user declined this. Nothing was changed.",
	}); err != nil {
		r.log.Error("could not record a declined tool call",
			slog.String("tool", c.Tool),
			slog.Any("error", err))
	}
}
