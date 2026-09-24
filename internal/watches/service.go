package watches

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/nudges/nudge"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Users resolves the account a watch belongs to: its timezone decides when
// "8:00" is.
type Users interface {
	ByID(ctx context.Context, id uuid.UUID) (users.User, error)
}

type Service struct {
	repo  *Repository
	users Users
	now   func() time.Time
}

func NewService(repo *Repository, accounts Users) *Service {
	return &Service{repo: repo, users: accounts, now: time.Now}
}

// WithClock fixes now. Tests only.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// Create stores a confirmed watch, first due at its next slot in the person's
// timezone. conversationID may be uuid.Nil; results then go to the latest chat.
func (s *Service) Create(ctx context.Context, userID, conversationID uuid.UUID, p Proposal) (Watch, error) {
	user, err := s.users.ByID(ctx, userID)
	if err != nil {
		return Watch{}, err
	}
	return s.repo.Create(ctx, userID, conversationID, p, p.Schedule.Next(s.now(), user.Location()))
}

func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]Watch, error) {
	return s.repo.List(ctx, userID)
}

// Runner runs one watch through the coach and posts the answer. spoke is
// false when the coach found nothing worth saying, in which case nothing was
// posted. The coach satisfies it; this package does not import the coach,
// which imports this one for the approval card.
type Runner interface {
	RunStandingTask(ctx context.Context, user users.User, conversationID uuid.UUID, instruction string) (msg conversations.Message, spoke bool, err error)
}

// Notifier raises the bell note and sends it everywhere the person can be
// reached (Telegram, Web Push). nudges.Service.RaiseProactive satisfies it.
type Notifier interface {
	RaiseProactive(ctx context.Context, user users.User, kind, dedupe, title, body, href string) error
}

// sweepBatch bounds one sweep. A watch left over waits fifteen minutes, which
// is the resolution the schedule is kept at anyway.
const sweepBatch = 100

// Sweeper is the worker-side entry point for KindSweepWatches.
type Sweeper struct {
	svc    *Service
	runner Runner
	notify Notifier
	log    *slog.Logger
}

func NewSweeper(svc *Service, runner Runner, notify Notifier, log *slog.Logger) *Sweeper {
	if log == nil {
		log = slog.Default()
	}
	return &Sweeper{svc: svc, runner: runner, notify: notify, log: log}
}

// HandleSweep runs every due watch once.
//
// Each run is claimed before the coach is asked anything: the claim moves the
// watch to its next slot with a conditional update, so a second worker, a
// duplicate periodic enqueue, or this job being retried finds nothing left to
// do. The cost of that order is that a run whose generation fails is skipped
// until its next slot rather than retried — for "tell me about my sleep", a
// missed morning is better than two messages.
func (s *Sweeper) HandleSweep(ctx context.Context, _ json.RawMessage) error {
	now := s.svc.now()
	due, err := s.svc.repo.Due(ctx, now, sweepBatch)
	if err != nil {
		return err
	}

	for _, w := range due {
		s.runOne(ctx, w, now)
	}
	return nil
}

func (s *Sweeper) runOne(ctx context.Context, w Watch, now time.Time) {
	log := s.log.With(slog.String("watch_id", w.ID.String()), slog.String("user_id", w.UserID.String()))

	user, err := s.svc.users.ByID(ctx, w.UserID)
	if err != nil {
		log.Warn("watch: could not load its owner", slog.Any("error", err))
		return
	}

	claimed, err := s.svc.repo.Claim(ctx, w.ID, w.NextRunAt, w.Schedule.Next(now, user.Location()), now)
	if err != nil {
		log.Warn("watch: could not claim its run", slog.Any("error", err))
		return
	}
	if !claimed {
		// Another sweep got there first. Not an error: it is the guard working.
		return
	}

	msg, spoke, err := s.runner.RunStandingTask(ctx, user, w.ConversationID, w.Instruction())
	if err != nil {
		log.Warn("watch: the coach could not run it", slog.Any("error", err))
		return
	}
	if !spoke {
		return
	}

	if s.notify == nil {
		return
	}
	title := conversations.SourceStandingTask + " · " + w.Title
	if err := s.notify.RaiseProactive(ctx, user, nudge.KindCoachReply, msg.ID.String(),
		title, preview(msg.Content), "/app/chat/"+msg.ConversationID.String()); err != nil {
		// The message is in the thread; a notification that did not go out
		// is logged, not retried.
		log.Warn("watch: could not notify", slog.Any("error", err))
	}
}

func preview(text string) string {
	text = strings.TrimSpace(text)
	if r := []rune(text); len(r) > 140 {
		return strings.TrimSpace(string(r[:140])) + "…"
	}
	return text
}
