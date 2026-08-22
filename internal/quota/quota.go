// Package quota bounds how often one account may take an action that costs
// real money or real work.
//
// It is the second of two limits North applies, and the two are not
// interchangeable. internal/shared/ratelimit keeps its buckets in memory and
// guards the bearer surfaces, where the job is to make a flood of cheap
// requests cost nothing to refuse. This package counts in Postgres and guards
// the session surfaces, where a single request reaches a paid model or fans out
// into extraction and embedding jobs. There, one small write alongside the work
// is free, and it is the only answer that stays correct once more than one
// replica is running — which is precisely when a spend limit begins to matter.
package quota

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// Action names something worth counting.
//
// A string rather than an enum so that guarding a new route is a line of Go
// rather than a migration, matching the same choice health_metrics.metric
// makes.
type Action string

const (
	CoachMessage    Action = "coach_message"
	DocumentUpload  Action = "document_upload"
	DocumentReindex Action = "document_reindex"
	ReportGenerate  Action = "report_generate"
	MediaAnalysis   Action = "media_analysis"
	AccountExport   Action = "account_export"
)

// Limit is one account's allowance for one action.
type Limit struct {
	PerWindow int
	Window    time.Duration
}

// DefaultWindow applies when a limit names a count but no window.
//
// An hour rather than a minute because these are human actions with bursty
// shapes: writing three messages in a row is normal, and a per-minute bound
// would punish the way people actually work.
const DefaultWindow = time.Hour

// Decision is the answer to one Consume.
//
// RetryAfter is only meaningful when Allowed is false; it is how long until the
// current window closes and the budget returns.
type Decision struct {
	Allowed    bool
	RetryAfter time.Duration
}

// Count is what a counter reports back: how much of the window is spent, and
// when that window began.
type Count struct {
	Used        int
	WindowStart time.Time
}

// Counter is the storage behind a quota, kept as an interface so the service
// can be tested against a counter that fails without needing a broken database.
type Counter interface {
	Consume(ctx context.Context, userID uuid.UUID, action Action, window time.Duration) (Count, error)
	Sweep(ctx context.Context, before time.Time) error
}

// Identity is who a request acts as, and on which plan.
//
// The tier travels with the account rather than being looked up here: the
// caller already holds it — every /app request has it on the session user, and
// the messaging path has it on the user it resolved from a link — so a lookup
// would be a second query for a value that is already in hand.
type Identity struct {
	UserID uuid.UUID
	Tier   string
}

// Identify names the account a request acts as.
//
// Passed in rather than read from internal/auth directly, so this package does
// not depend on the session machinery to count. That keeps the dependency
// pointing the way the architecture says it should, and it is what lets the
// middleware be tested without building a session.
type Identify func(ctx context.Context) (Identity, bool)

type Service struct {
	counter  Counter
	limits   Limits
	identify Identify
	now      func() time.Time
	log      *slog.Logger
}

// NewService returns a quota service.
//
// identify may be nil for a service that is only used through Consume, where
// the caller already knows the account; Guard requires it.
func NewService(counter Counter, limits Limits, identify Identify) *Service {
	return &Service{counter: counter, limits: limits, identify: identify, now: time.Now, log: slog.Default()}
}

// Consume counts one request against an action's budget and reports whether it
// may proceed.
//
// The tier selects which budget applies. It is a parameter rather than
// something read from the context because the two callers that matter reach
// this from different places — an HTTP request and a Telegram update — and a
// context-carried tier would resolve to the empty string on the second, quietly
// serving a paying customer the free ceiling.
//
// # Why a failure is allowed through
//
// A counter that cannot be reached returns an allowed decision and no error.
// The limiter exists to protect the application from a caller; it must not
// become a way for the database to take every guarded page offline at once. The
// operator learns about it from the caller's log, not from an outage.
//
// # Why a refused request still costs its slot
//
// The count is incremented before the comparison, so a refused request pushes
// the total further past the limit rather than sitting at it. Against a retry
// loop — the case this is built for — that is the behaviour you want: the loop
// does not get a free probe on every iteration.
func (s *Service) Consume(ctx context.Context, userID uuid.UUID, tier string, action Action) (Decision, error) {
	limit, ok := s.limits.For(tier)[action]
	if !ok || limit.PerWindow <= 0 {
		// No configured budget is not a budget of zero. Guarding a route must
		// never be what takes it offline.
		return Decision{Allowed: true}, nil
	}

	count, err := s.counter.Consume(ctx, userID, action, limit.Window)
	if err != nil {
		s.log.Error("quota counter unavailable; allowing the request",
			slog.String("action", string(action)),
			slog.String("tier", tier),
			slog.String("user_id", userID.String()),
			slog.Any("error", err))
		return Decision{Allowed: true}, nil
	}

	if count.Used <= limit.PerWindow {
		return Decision{Allowed: true}, nil
	}

	retryAfter := count.WindowStart.Add(limit.Window).Sub(s.now())
	if retryAfter <= 0 {
		// The window closed between the write and this line. Telling the client
		// to wait zero seconds reads as a bug, so round up to the smallest
		// answer that is still true.
		retryAfter = time.Second
	}
	return Decision{Allowed: false, RetryAfter: retryAfter}, nil
}

// Limit reports the configured budget for a tier's action, and whether one
// exists.
func (s *Service) Limit(tier string, action Action) (Limit, bool) {
	limit, ok := s.limits.For(tier)[action]
	return limit, ok
}
