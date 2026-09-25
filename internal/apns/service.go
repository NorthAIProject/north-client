package apns

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/config"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// maxBodyRunes keeps the payload well under Apple's 4 KiB limit. Nudge
// bodies are a sentence; this is a guard.
const maxBodyRunes = 500

// Service keeps device tokens and sends to them.
//
// A Service built with a nil Sender is APNs switched off: Register refuses
// and Send reaches nobody, so a deployment without a key never stores tokens
// it cannot use.
type Service struct {
	repo   *Repository
	sender Sender
	topics []string
	log    *slog.Logger
}

func NewService(repo *Repository, sender Sender, topics []string, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{repo: repo, sender: sender, topics: topics, log: log}
}

// Enabled reports whether this deployment can send at all.
func (s *Service) Enabled() bool {
	return s != nil && s.sender != nil && len(s.topics) > 0
}

// Register stores the token the app was handed.
func (s *Service) Register(ctx context.Context, userID uuid.UUID, in Input) (Device, error) {
	if !s.Enabled() {
		return Device{}, apperr.ErrUnavailable
	}
	in = in.normalized()
	if err := in.validate(s.topics); err != nil {
		return Device{}, err
	}
	return s.repo.Upsert(ctx, userID, in)
}

// Unregister forgets one of the person's devices. Forgetting a token that is
// already gone is success: the outcome they asked for holds.
func (s *Service) Unregister(ctx context.Context, userID uuid.UUID, token string) error {
	return s.repo.DeleteByToken(ctx, userID, Input{Token: token}.normalized().Token)
}

// payload is the JSON Apple delivers to the app. href rides beside aps, where
// the app reads it from the notification's userInfo.
type payload struct {
	APS  aps    `json:"aps"`
	Href string `json:"href,omitempty"`
}

type aps struct {
	Alert    alert  `json:"alert"`
	Sound    string `json:"sound"`
	ThreadID string `json:"thread-id"`
	// Category names the iOS notification category, which decides the
	// buttons shown under the banner. The app registers the same names.
	Category string `json:"category,omitempty"`
}

type alert struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
}

// Send delivers one notification to every device this person registered and
// reports how many Apple accepted.
//
// Its signature matches the browser push service, so the nudge engine treats
// the two as interchangeable channels. Every path that sends nothing logs why:
// a banner that never arrives is otherwise impossible to diagnose.
func (s *Service) Send(ctx context.Context, userID uuid.UUID, title, body, href, category string) (int, error) {
	if !s.Enabled() {
		return 0, nil
	}
	devices, err := s.repo.ListByUser(ctx, userID)
	if err != nil {
		return 0, err
	}
	if len(devices) == 0 {
		s.log.Info("apns: no devices registered", slog.String("user_id", userID.String()))
		return 0, nil
	}

	raw, err := json.Marshal(payload{
		APS:  aps{Alert: alert{Title: title, Body: clip(body, maxBodyRunes)}, Sound: "default", ThreadID: "nudges", Category: category},
		Href: href,
	})
	if err != nil {
		return 0, apperr.Wrap(err, "encode apns payload")
	}

	delivered := 0
	for _, d := range devices {
		res, sendErr := s.sender.Send(ctx, d, raw)
		attrs := []any{
			slog.String("user_id", userID.String()),
			slog.String("device_id", d.ID.String()),
			slog.String("topic", d.Topic),
			slog.String("environment", d.Environment),
		}
		switch {
		case sendErr != nil:
			s.log.Warn("apns: could not reach apple", append(attrs, slog.Any("error", sendErr))...)
			s.mark(ctx, s.repo.MarkFailed, d.ID)
		case res.OK():
			delivered++
			s.log.Info("apns: delivered", attrs...)
			s.mark(ctx, s.repo.MarkUsed, d.ID)
		case res.Gone():
			s.log.Info("apns: device gone, forgetting it",
				append(attrs, slog.Int("status", res.Status), slog.String("reason", res.Reason))...)
			s.mark(ctx, s.repo.Delete, d.ID)
		default:
			s.log.Warn("apns: apple refused the notification",
				append(attrs, slog.Int("status", res.Status), slog.String("reason", res.Reason))...)
			s.mark(ctx, s.repo.MarkFailed, d.ID)
		}
	}
	return delivered, nil
}

// mark applies a bookkeeping update and logs rather than fails: the send
// already happened, and its outcome is not undone by a bookkeeping error.
func (s *Service) mark(ctx context.Context, fn func(context.Context, uuid.UUID) error, id uuid.UUID) {
	if err := fn(ctx, id); err != nil {
		s.log.Warn("apns: could not record delivery outcome",
			slog.Any("error", err),
			slog.String("device_id", id.String()))
	}
}

func clip(s string, runes int) string {
	r := []rune(s)
	if len(r) <= runes {
		return s
	}
	return string(r[:runes-1]) + "…"
}

// FromConfig builds the service a deployment asked for. Without a key it is
// switched off rather than failing the boot, like Web Push without VAPID.
func FromConfig(repo *Repository, cfg config.APNsConfig, log *slog.Logger) (*Service, error) {
	if !cfg.Enabled() {
		return NewService(repo, nil, nil, log), nil
	}
	sender, err := NewHTTPSender(cfg.KeyID, cfg.TeamID, cfg.PrivateKey)
	if err != nil {
		return nil, err
	}
	return NewService(repo, sender, cfg.Topics, log), nil
}
