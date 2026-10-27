package checkinreply

import (
	"context"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/checkins/checkin"
	"github.com/NorthAIProject/north-client/internal/quota"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/users"
)

const (
	// defaultTimeout bounds the whole provider walk. The app and the page show
	// a placeholder while they wait; past this the note is not worth waiting
	// for.
	defaultTimeout = 8 * time.Second
	// maxRunes is the note's length rule, enforced again here: the prompt
	// asks, this decides.
	maxRunes = 200
	// historyLimit is how many check-ins are read: a month of daily use.
	historyLimit = 30
	// recentShown is how many earlier days the model sees, the same fortnight
	// the coach's context carries.
	recentShown = 14
)

// Prompt is what the model is given to write one note.
type Prompt struct {
	// Today is today's check-in, every word of it.
	Today string
	// Recent are earlier check-ins, newest first.
	Recent []string
	// Pattern is a sentence Go found in the check-ins, on the days one is
	// looked for. Empty otherwise.
	Pattern string
}

// Writer turns a prompt into a note. The production one is the model.
type Writer interface {
	Write(ctx context.Context, user users.User, p Prompt) (string, error)
}

// CheckIns is the part of the check-ins service a note reads.
type CheckIns interface {
	Today(ctx context.Context, user users.User) (checkin.CheckIn, error)
	List(ctx context.Context, userID uuid.UUID, limit int) ([]checkin.CheckIn, error)
	Total(ctx context.Context, userID uuid.UUID) (int, error)
}

// Quota bounds how many notes a person can cost. Only a note that has to be
// written spends one; a stored note is free.
type Quota interface {
	Consume(ctx context.Context, userID uuid.UUID, tier string, action quota.Action) (quota.Decision, error)
}

type Service struct {
	checkins CheckIns
	repo     *Repository
	writer   Writer
	quota    Quota
	timeout  time.Duration
}

func NewService(checkins CheckIns, repo *Repository, writer Writer) *Service {
	return &Service{checkins: checkins, repo: repo, writer: writer, timeout: defaultTimeout}
}

// WithQuota bounds the notes written per person. Without one, none is.
func (s *Service) WithQuota(q Quota) *Service {
	c := *s
	c.quota = q
	return &c
}

// WithTimeout changes how long a note may take to write.
func (s *Service) WithTimeout(d time.Duration) *Service {
	c := *s
	c.timeout = d
	return &c
}

// lookup is today's check-in and the note stored for its current content.
// hasToday is false before today's check-in exists; ok is false when no note
// matches what it now says.
func (s *Service) lookup(ctx context.Context, user users.User) (today checkin.CheckIn, reply Reply, hasToday, ok bool, err error) {
	today, err = s.checkins.Today(ctx, user)
	if apperr.Is(err, apperr.ErrNotFound) {
		return checkin.CheckIn{}, Reply{}, false, false, nil
	}
	if err != nil {
		return checkin.CheckIn{}, Reply{}, false, false, err
	}
	got, found, err := s.repo.Get(ctx, today.ID, user.ID)
	if err != nil {
		return today, Reply{}, true, false, err
	}
	if found && got.ContentHash == contentHash(today) {
		return today, got.Reply, true, true, nil
	}
	return today, Reply{}, true, false, nil
}

// Stored is the note already written for today's check-in as it now reads.
// It never writes one.
func (s *Service) Stored(ctx context.Context, user users.User) (Reply, bool, error) {
	_, reply, _, ok, err := s.lookup(ctx, user)
	return reply, ok, err
}

// ForToday is the note under today's check-in, written now if it has to be.
// ok is false when there is nothing to show: no check-in yet, or a note that
// could not be written. An error is only a failure to read or store, never
// the model's.
func (s *Service) ForToday(ctx context.Context, user users.User) (Reply, bool, error) {
	today, stored, hasToday, ok, err := s.lookup(ctx, user)
	if err != nil || !hasToday || ok {
		return stored, ok, err
	}
	hash := contentHash(today)

	log := middleware.FromContext(ctx).With(slog.String("user_id", user.ID.String()))
	if s.quota != nil {
		decision, quotaErr := s.quota.Consume(ctx, user.ID, string(user.Tier), quota.CheckInReply)
		if quotaErr == nil && !decision.Allowed {
			return Reply{}, false, nil
		}
	}

	prompt, kind, err := s.prompt(ctx, user, today)
	if err != nil {
		return Reply{}, false, err
	}
	text, ok := s.write(ctx, log, user, prompt)
	if !ok {
		return Reply{}, false, nil
	}

	reply := Reply{Kind: kind, Text: text}
	if err = s.repo.Put(ctx, today.ID, user.ID, hash, reply); err != nil {
		// Written and paid for: show it, and write another next time.
		log.Warn("check-in reply not stored", slog.Any("error", err))
	}
	return reply, true, nil
}

// prompt gathers today's check-in, the days before it and, on the check-ins
// where one is looked for, a pattern.
func (s *Service) prompt(ctx context.Context, user users.User, today checkin.CheckIn) (Prompt, string, error) {
	history, err := s.checkins.List(ctx, user.ID, historyLimit)
	if err != nil {
		return Prompt{}, "", err
	}
	p := Prompt{Today: inFull(today)}
	for _, c := range history {
		if c.ID != today.ID && len(p.Recent) < recentShown {
			p.Recent = append(p.Recent, c.Summary())
		}
	}

	total, err := s.checkins.Total(ctx, user.ID)
	if err != nil {
		return Prompt{}, "", err
	}
	if patternAt[total] {
		if pattern, found := strongestPattern(history); found {
			p.Pattern = pattern
			return p, KindPattern, nil
		}
	}
	return p, KindDaily, nil
}

// write asks for the note within the deadline and applies the rules the
// prompt states. ok is false for any failure.
func (s *Service) write(ctx context.Context, log *slog.Logger, user users.User, p Prompt) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	raw, err := s.writer.Write(ctx, user, p)
	if err != nil {
		log.Warn("check-in reply not written", slog.Any("error", err))
		return "", false
	}
	text, ok := clean(raw)
	if !ok {
		log.Warn("check-in reply dropped", slog.Int("runes", utf8.RuneCountInString(raw)))
	}
	return text, ok
}

// clean turns a model reply into the plain one-line note the card shows, or
// reports it unusable: empty, or longer than the rule allows.
func clean(raw string) (string, bool) {
	text := strings.NewReplacer("**", "", "__", "", "`", "", "#", "").Replace(raw)
	text = strings.Join(strings.Fields(text), " ")
	text = strings.Trim(text, `"“”'`)
	if text == "" || utf8.RuneCountInString(text) > maxRunes {
		return "", false
	}
	return text, true
}
