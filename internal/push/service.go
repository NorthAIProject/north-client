package push

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// funnel is what this package reports to the product funnel: that a browser
// opted in. Optional.
type funnel interface {
	PushSubscribed(ctx context.Context, userID uuid.UUID)
}

// Service keeps subscriptions and sends to them.
//
// A Service built with a nil Sender is push switched off: Enabled reports
// false, Send delivers to nobody and reports zero, and Subscribe refuses. The
// settings page and the dashboard read Enabled to decide whether to offer the
// button at all, so a deployment without VAPID keys never shows a control that
// cannot work.
type Service struct {
	repo   *Repository
	sender Sender
	vapid  VAPID
	funnel funnel
	log    *slog.Logger
}

func NewService(repo *Repository, sender Sender, vapid VAPID, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{repo: repo, sender: sender, vapid: vapid, log: log}
}

// WithFunnel reports opt-ins to the product funnel.
func (s *Service) WithFunnel(f funnel) *Service {
	s.funnel = f
	return s
}

// Enabled reports whether this deployment can send at all.
func (s *Service) Enabled() bool {
	return s != nil && s.sender != nil && s.vapid.Enabled()
}

// PublicKey is the applicationServerKey the browser subscribes with.
func (s *Service) PublicKey() string {
	if s == nil {
		return ""
	}
	return s.vapid.PublicKey
}

// Subscribe stores what the browser handed over.
//
// The opt-in is reported to the funnel on every call rather than only on a
// new endpoint: a browser re-subscribes only when its keys rotate, which is
// rare enough that counting it twice costs less than the query to tell the
// two apart.
func (s *Service) Subscribe(ctx context.Context, userID uuid.UUID, in Input) (Subscription, error) {
	if !s.Enabled() {
		return Subscription{}, apperr.ErrUnavailable
	}
	if err := in.validate(); err != nil {
		return Subscription{}, err
	}
	sub, err := s.repo.Upsert(ctx, userID, in)
	if err != nil {
		return Subscription{}, err
	}
	if s.funnel != nil {
		s.funnel.PushSubscribed(ctx, userID)
	}
	return sub, nil
}

// Unsubscribe forgets one of the person's browsers. Forgetting an endpoint
// that is already gone is success: the outcome they asked for holds.
func (s *Service) Unsubscribe(ctx context.Context, userID uuid.UUID, endpoint string) error {
	_, err := s.repo.DeleteByEndpoint(ctx, userID, endpoint)
	return err
}

// Count is how many browsers this person has subscribed.
func (s *Service) Count(ctx context.Context, userID uuid.UUID) (int, error) {
	if !s.Enabled() {
		return 0, nil
	}
	return s.repo.Count(ctx, userID)
}

// HasSubscription reports whether any browser of theirs can be reached.
func (s *Service) HasSubscription(ctx context.Context, userID uuid.UUID) (bool, error) {
	n, err := s.Count(ctx, userID)
	return n > 0, err
}

// Send delivers one notification to every browser this person subscribed and
// reports how many accepted it.
//
// A subscription the push service declares gone (404 or 410) is deleted on
// the spot: the browser has unsubscribed or been reset, and every later send
// would fail the same way. Any other refusal, or no answer at all, stamps the
// row as failed and moves on. The nudge the caller stored is never at stake
// here, which is why this returns an error only when the subscriptions could
// not be read.
func (s *Service) Send(ctx context.Context, userID uuid.UUID, title, body, href string) (int, error) {
	if !s.Enabled() {
		return 0, nil
	}

	subs, err := s.repo.ListByUser(ctx, userID)
	if err != nil {
		return 0, err
	}
	if len(subs) == 0 {
		return 0, nil
	}

	payload, err := json.Marshal(message{Title: title, Body: clip(body, maxBodyRunes), Href: href})
	if err != nil {
		return 0, apperr.Wrap(err, "encode push payload")
	}

	delivered := 0
	for _, sub := range subs {
		status, sendErr := s.sender.Send(ctx, sub, payload)
		switch {
		case sendErr != nil:
			s.log.Warn("push: could not reach the push service",
				slog.Any("error", sendErr),
				slog.String("user_id", userID.String()))
			s.mark(ctx, s.repo.MarkFailed, sub.ID)

		case status == http.StatusNotFound || status == http.StatusGone:
			s.log.Info("push: subscription gone, forgetting it",
				slog.Int("status", status),
				slog.String("user_id", userID.String()))
			s.mark(ctx, s.repo.Delete, sub.ID)

		case status >= 200 && status < 300:
			delivered++
			s.mark(ctx, s.repo.MarkUsed, sub.ID)

		default:
			s.log.Warn("push: push service refused the message",
				slog.Int("status", status),
				slog.String("user_id", userID.String()))
			s.mark(ctx, s.repo.MarkFailed, sub.ID)
		}
	}
	return delivered, nil
}

// mark applies a bookkeeping update and logs rather than fails: the send
// already happened, and its outcome is not undone by a bookkeeping error.
func (s *Service) mark(ctx context.Context, fn func(context.Context, uuid.UUID) error, id uuid.UUID) {
	if err := fn(ctx, id); err != nil {
		s.log.Warn("push: could not record delivery outcome",
			slog.Any("error", err),
			slog.String("subscription_id", id.String()))
	}
}
