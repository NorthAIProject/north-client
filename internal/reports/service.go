package reports

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/jobs"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// cooldown is how long a finished review must sit before it can be generated
// again. Linear asked for a rate limit; one active row per week plus this
// window is the rule.
const cooldown = time.Hour

// ErrArchived is returned when somebody asks to regenerate a review they have
// already archived. A conflict rather than a validation failure: the request is
// well-formed, it is the state of the row that refuses it.
var ErrArchived = apperr.Wrap(apperr.ErrConflict, "That review is archived.")

// Enqueuer schedules background work. *jobs.Queue satisfies it.
type Enqueuer interface {
	Enqueue(ctx context.Context, kind jobs.Kind, payload any) (jobs.Job, error)
}

// Users looks up the account so week bounds use their timezone.
type Users interface {
	ByID(ctx context.Context, id uuid.UUID) (users.User, error)
}

type Service struct {
	repo     *Repository
	users    Users
	queue    Enqueuer
	client   Generator
	context  ContextLoader
	now      func() time.Time
	cooldown time.Duration
}

type Options struct {
	Repository *Repository
	Users      Users
	Queue      Enqueuer
	Client     Generator
	Context    ContextLoader
	Now        func() time.Time
	Cooldown   time.Duration
}

func NewService(opts Options) *Service {
	s := &Service{
		repo:     opts.Repository,
		users:    opts.Users,
		queue:    opts.Queue,
		client:   opts.Client,
		context:  opts.Context,
		now:      opts.Now,
		cooldown: opts.Cooldown,
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.cooldown <= 0 {
		s.cooldown = cooldown
	}
	return s
}

func (s *Service) Get(ctx context.Context, id, userID uuid.UUID) (Report, error) {
	return s.repo.Get(ctx, id, userID)
}

func (s *Service) List(ctx context.Context, userID uuid.UUID, includeArchived bool) ([]Report, error) {
	return s.repo.List(ctx, userID, includeArchived, 50)
}

func (s *Service) Archive(ctx context.Context, id, userID uuid.UUID) error {
	_, err := s.repo.Archive(ctx, id, userID)
	return err
}

// RequestGenerate creates or reuses this week's review and queues generation.
//
// weekStart may be zero, in which case "this week" is the week that contains
// now in the user's timezone.
func (s *Service) RequestGenerate(ctx context.Context, userID uuid.UUID, weekStart time.Time) (Report, error) {
	user, err := s.users.ByID(ctx, userID)
	if err != nil {
		return Report{}, err
	}

	at := s.now()
	if !weekStart.IsZero() {
		at = weekStart
	}
	week := WeekContaining(at, user.Location())

	existing, err := s.repo.ActiveForWeek(ctx, userID, KindWeekly, week.Start)
	switch {
	case err == nil:
		if existing.GeneratedAt != nil && s.now().Sub(*existing.GeneratedAt) < s.cooldown {
			return Report{}, apperr.Wrap(apperr.ErrConflict, "Already generated this hour.")
		}
		// A pending row already has a job behind it. Without this, the cooldown
		// above — which only looks at GeneratedAt — lets somebody who reloads
		// the form stack an unbounded number of generations for one week, since
		// a report that has never finished has no GeneratedAt to rate-limit
		// against. Past the cooldown the job is presumed lost and requeued.
		//
		// A failed row is deliberately not covered: a provider having a bad
		// minute must not lock the week for an hour.
		if existing.Status == StatusPending && s.now().Sub(existing.UpdatedAt) < s.cooldown {
			return existing, nil
		}
		if existing.Status != StatusPending {
			if existing, err = s.repo.MarkPending(ctx, existing.ID, userID); err != nil {
				return Report{}, err
			}
		}
		if err = s.enqueue(ctx, existing); err != nil {
			return Report{}, err
		}
		return existing, nil
	case apperr.Is(err, apperr.ErrNotFound):
		var created Report
		created, err = s.repo.Create(ctx, userID, week, KindWeekly)
		if err != nil {
			return Report{}, err
		}
		if err = s.enqueue(ctx, created); err != nil {
			return Report{}, err
		}
		return created, nil
	default:
		return Report{}, err
	}
}

// EnsureWeekly creates and enqueues the review for one week, and does nothing
// at all when that week already has one.
//
// This is the scheduled path, and it deliberately does not go through
// RequestGenerate: that method exists to serve somebody pressing a button, so
// past its cooldown it marks a finished review pending and generates it again.
// Correct for a person asking twice; wrong for a sweep that will come round
// every hour and would rewrite the same week all day.
//
// created is false when the week already had a review, whatever its status.
func (s *Service) EnsureWeekly(ctx context.Context, userID uuid.UUID, week Week) (Report, bool, error) {
	_, err := s.repo.ActiveForWeek(ctx, userID, KindWeekly, week.Start)
	switch {
	case err == nil:
		return Report{}, false, nil
	case !apperr.Is(err, apperr.ErrNotFound):
		return Report{}, false, err
	}

	created, err := s.repo.Create(ctx, userID, week, KindWeekly)
	if err != nil {
		return Report{}, false, err
	}
	if err := s.enqueue(ctx, created); err != nil {
		return Report{}, false, err
	}
	return created, true, nil
}

// Regenerate re-runs the review somebody is looking at.
//
// Refusing an archived one is the point. Archiving is how a person says they
// are finished with a week, and ActiveForWeek deliberately cannot see archived
// rows — so without this check RequestGenerate finds nothing for that week and
// quietly creates a *second* review of it, which reads as the archive having
// been ignored.
func (s *Service) Regenerate(ctx context.Context, id, userID uuid.UUID) (Report, error) {
	current, err := s.repo.Get(ctx, id, userID)
	if err != nil {
		return Report{}, err
	}
	if current.Archived() {
		return Report{}, ErrArchived
	}
	return s.RequestGenerate(ctx, userID, current.PeriodStart)
}

func (s *Service) enqueue(ctx context.Context, report Report) error {
	if s.queue == nil {
		return nil
	}
	_, err := s.queue.Enqueue(ctx, jobs.KindWeeklyReview, jobs.WeeklyReviewPayload{
		UserID:   report.UserID,
		ReportID: report.ID,
	})
	return err
}

// HandleGenerateJob is the worker entry point.
func (s *Service) HandleGenerateJob(ctx context.Context, payload json.RawMessage) error {
	var p jobs.WeeklyReviewPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return apperr.Wrap(err, "decode weekly review payload")
	}
	if p.UserID == uuid.Nil || p.ReportID == uuid.Nil {
		return apperr.New("weekly review payload missing ids")
	}
	return s.Generate(ctx, p.ReportID, p.UserID)
}

func (s *Service) LatestReady(ctx context.Context, userID uuid.UUID) (Report, error) {
	return s.repo.LatestReady(ctx, userID)
}
