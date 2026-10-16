package outreach

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/conversations"
	"github.com/NorthAIProject/north-client/internal/lighterday"
	"github.com/NorthAIProject/north-client/internal/messaging"
	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/nudges"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts"
)

// Prefs is what the person allows. notifications.Service satisfies it.
type Prefs interface {
	Get(ctx context.Context, userID uuid.UUID) (notifications.Prefs, error)
}

// Lighter is the lighter-day choice. lighterday.Service satisfies it.
type Lighter interface {
	Today(ctx context.Context, user users.User) (lighterday.Today, error)
	ChooseAs(ctx context.Context, user users.User, choice, source string) (lighterday.Today, error)
}

// Training finds and moves a missed session. workouts.Service satisfies it.
type Training interface {
	MissedYesterday(ctx context.Context, user users.User, now time.Time) (workouts.Missed, bool, error)
	MoveMissed(ctx context.Context, user users.User, now time.Time, to string) (workouts.WeekProgress, error)
}

// Writer puts the message in the coach's voice and posts it.
// coach.Service satisfies it.
type Writer interface {
	RunOutreach(ctx context.Context, user users.User, situation string) (conversations.Message, error)
}

// Chats posts a message into the coach thread without a model: the fallback
// when the writer failed. conversations.Service satisfies it.
type Chats interface {
	PostProactive(ctx context.Context, userID, preferred uuid.UUID, text, sourceLabel string) (conversations.Message, error)
}

// Notifier raises the bell note and the push. nudges.Service satisfies it.
type Notifier interface {
	Raise(ctx context.Context, user users.User, d nudges.Draft) (nudges.Nudge, bool, error)
}

// Chat sends the message, with its buttons, to the linked Telegram chat, and
// edits it once answered. messaging.Service satisfies it.
type Chat interface {
	NotifyTaggedMessage(ctx context.Context, userID uuid.UUID, kind, dedupeKey string, msg messaging.OutboundMessage) error
	ResolveTagged(ctx context.Context, userID uuid.UUID, kind, dedupeKey, updatedText string) error
}

// Users pages the accounts the sweep visits. users.Service satisfies it.
type Users interface {
	ListOnboarded(ctx context.Context, after uuid.UUID, limit int) ([]users.User, error)
}

// Options are the service's collaborators. Lighter and Training may be nil,
// which turns their trigger off; the rest are needed.
type Options struct {
	Repository *Repository
	Prefs      Prefs
	Lighter    Lighter
	Training   Training
	Writer     Writer
	Chats      Chats
	Notifier   Notifier
	Chat       Chat
	Users      Users
	Log        *slog.Logger
}

type Service struct {
	repo     *Repository
	prefs    Prefs
	lighter  Lighter
	training Training
	writer   Writer
	chats    Chats
	notifier Notifier
	chat     Chat
	users    Users
	log      *slog.Logger
	now      func() time.Time
}

func NewService(o Options) *Service {
	s := &Service{
		repo: o.Repository, prefs: o.Prefs, lighter: o.Lighter, training: o.Training,
		writer: o.Writer, chats: o.Chats, notifier: o.Notifier, chat: o.Chat, users: o.Users,
		log: o.Log, now: time.Now,
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	return s
}

// WithClock fixes now, for tests.
func (s *Service) WithClock(now func() time.Time) *Service { s.now = now; return s }

// The hours, local, in which each trigger may speak: a low morning while
// the session is still ahead, a missed session once the day is under way.
const (
	recoveryFrom, recoveryUntil = 7, 12
	missedFrom, missedUntil     = 9, 20
)

// Evaluate sends today's outreach for one person if something warrants it.
// sent is false when nothing did, or the day already had one.
func (s *Service) Evaluate(ctx context.Context, user users.User) (sent bool, err error) {
	ctx = i18n.WithLocale(ctx, string(user.Locale))
	prefs, err := s.prefs.Get(ctx, user.ID)
	if err != nil {
		return false, err
	}
	local := s.now().In(user.Location())
	if !prefs.CoachCheckIns || prefs.InQuietHours(local) {
		return false, nil
	}
	day := timerange.StartOfDay(local)
	if _, err = s.repo.ForDay(ctx, user.ID, day); err == nil {
		return false, nil
	} else if !apperr.Is(err, apperr.ErrNotFound) {
		return false, err
	}

	o, ok, err := s.lowRecovery(ctx, user, prefs, local)
	if err == nil && !ok {
		o, ok, err = s.missedSession(ctx, user, local)
	}
	if err != nil || !ok {
		return false, err
	}
	s.send(ctx, user, o)
	return true, nil
}

func (s *Service) lowRecovery(ctx context.Context, user users.User, prefs notifications.Prefs, local time.Time) (Outreach, bool, error) {
	if s.lighter == nil || local.Hour() < recoveryFrom || local.Hour() >= recoveryUntil {
		return Outreach{}, false, nil
	}
	t, err := s.lighter.Today(ctx, user)
	if err != nil || !t.Offered() {
		return Outreach{}, false, err
	}
	facts := Facts{Session: t.Session, Reason: t.Recovery.WhyIn(ctx)}
	if prefs.CoachAdjustsTraining {
		// Done before the claim: a lighter day with no message is harmless,
		// a message about a change that was never made is not.
		if _, err = s.lighter.ChooseAs(ctx, user, lighterday.ChoiceLighter, lighterday.SourceCoach); err != nil {
			return Outreach{}, false, err
		}
		facts.Applied = true
	}
	return s.repo.Claim(ctx, user.ID, timerange.StartOfDay(local), TriggerLowRecovery, facts)
}

func (s *Service) missedSession(ctx context.Context, user users.User, local time.Time) (Outreach, bool, error) {
	if s.training == nil || local.Hour() < missedFrom || local.Hour() >= missedUntil {
		return Outreach{}, false, nil
	}
	m, ok, err := s.training.MissedYesterday(ctx, user, local)
	if err != nil || !ok {
		return Outreach{}, false, err
	}
	facts := Facts{Session: m.Title, Weekday: m.Weekday, TodayFree: m.TodayFree, TomorrowFree: m.TomorrowFree}
	return s.repo.Claim(ctx, user.ID, timerange.StartOfDay(local), TriggerMissedSession, facts)
}

// send writes the message in the coach's voice, or falls back to the
// catalogue's sentence, and delivers it: the coach thread, the bell and push
// (with the answers under the banner), and Telegram with buttons. Nothing
// here fails Evaluate: the outreach is claimed, and a channel that did not
// work is logged.
func (s *Service) send(ctx context.Context, user users.User, o Outreach) {
	var msg conversations.Message
	var err error
	if s.writer != nil {
		msg, err = s.writer.RunOutreach(ctx, user, situation(o))
	}
	if s.writer == nil || err != nil {
		if err != nil {
			s.log.Warn("outreach: coach could not write, using the fallback", "user_id", user.ID, "error", err)
		}
		msg, err = s.chats.PostProactive(ctx, user.ID, uuid.Nil, fallback(ctx, o), conversations.SourceCoachCheckIn)
		if err != nil {
			s.log.Error("outreach: could not post to the coach thread", "user_id", user.ID, "error", err)
			msg = conversations.Message{Content: fallback(ctx, o)}
		}
	}
	o.Body = msg.Content
	var messageID *uuid.UUID
	if msg.ID != uuid.Nil {
		messageID = &msg.ID
	}
	if err := s.repo.SetMessage(ctx, o.ID, user.ID, o.Body, messageID); err != nil {
		s.log.Error("outreach: could not save the message", "user_id", user.ID, "error", err)
	}

	// Push only: Telegram gets its own copy below, with buttons.
	if _, _, err := s.notifier.Raise(ctx, user, nudges.Draft{
		Kind: o.NudgeKind(), DedupeKey: o.ID.String(),
		Title: i18n.T(ctx, "outreach.title"), Body: o.Body, Href: "/app", Push: true,
	}); err != nil {
		s.log.Warn("outreach: could not raise the note", "user_id", user.ID, "error", err)
	}
	if s.chat != nil {
		if err := s.chat.NotifyTaggedMessage(ctx, user.ID, o.NudgeKind(), o.ID.String(), messaging.OutboundMessage{
			Text: o.Body, Options: chatOptions(ctx, o),
		}); err != nil {
			s.log.Warn("outreach: could not send to the linked chat", "user_id", user.ID, "error", err)
		}
	}
}

// Today is the person's outreach for their local today, if there is one.
func (s *Service) Today(ctx context.Context, user users.User) (Outreach, bool, error) {
	o, err := s.repo.ForDay(ctx, user.ID, timerange.StartOfDay(s.now().In(user.Location())))
	if apperr.Is(err, apperr.ErrNotFound) {
		return Outreach{}, false, nil
	}
	return o, err == nil, err
}

// Act applies the person's answer. The first answer counts; the outreach
// must be today's, since moving a session or undoing a lighter day only means
// anything on the day it was offered.
func (s *Service) Act(ctx context.Context, user users.User, id uuid.UUID, action string) (Outreach, error) {
	o, err := s.repo.Get(ctx, id, user.ID)
	if err != nil {
		return Outreach{}, err
	}
	return s.act(ctx, user, o, action)
}

// ActToday answers today's outreach: what a notification button sends, since
// a banner knows the day but not the row.
func (s *Service) ActToday(ctx context.Context, user users.User, action string) (Outreach, error) {
	o, ok, err := s.Today(ctx, user)
	if err != nil {
		return Outreach{}, err
	}
	if !ok {
		return Outreach{}, apperr.ErrNotFound
	}
	return s.act(ctx, user, o, action)
}

func (s *Service) act(ctx context.Context, user users.User, o Outreach, action string) (Outreach, error) {
	ctx = i18n.WithLocale(ctx, string(user.Locale))
	now := s.now().In(user.Location())
	if !sameDay(o.Day, now) {
		return Outreach{}, apperr.Wrap(apperr.ErrConflict, "this was for another day")
	}
	if !o.Offers(action) {
		return Outreach{}, apperr.FieldErrors{}.Add("action", i18n.T(ctx, "outreach.error.action"))
	}
	if o.Answered() {
		return Outreach{}, apperr.Wrap(apperr.ErrConflict, "this was already answered")
	}

	var err error
	switch action {
	case ActionUndo, ActionKeep:
		_, err = s.lighter.ChooseAs(ctx, user, lighterday.ChoiceKeep, lighterday.SourcePerson)
	case ActionLighter:
		_, err = s.lighter.ChooseAs(ctx, user, lighterday.ChoiceLighter, lighterday.SourcePerson)
	case ActionToday:
		_, err = s.training.MoveMissed(ctx, user, now, workouts.MoveToday)
	case ActionTomorrow:
		_, err = s.training.MoveMissed(ctx, user, now, workouts.MoveTomorrow)
	case ActionLetGo:
		// The rotation already carries an untrained session forward.
	}
	if err != nil {
		return Outreach{}, err
	}
	answered, err := s.repo.RecordAction(ctx, o.ID, user.ID, action)
	if err != nil {
		return Outreach{}, err
	}
	if s.chat != nil {
		text := strings.TrimSpace(o.Body + "\n\n" + Outcome(ctx, action))
		if err := s.chat.ResolveTagged(ctx, user.ID, o.NudgeKind(), o.ID.String(), text); err != nil {
			s.log.Warn("outreach: could not update the linked chat", "user_id", user.ID, "error", err)
		}
	}
	return answered, nil
}

// ChatPrefix starts the value of every outreach button sent to a chat:
// "outreach:<id>:<action>".
const ChatPrefix = "outreach:"

// AnswerFromChat handles a tapped chat button. handled is false for any
// text that is not an outreach answer, which then goes on to the coach as
// usual. It satisfies messaging.OutreachAnswers.
func (s *Service) AnswerFromChat(ctx context.Context, user users.User, text string) (reply string, handled bool) {
	rest, ok := strings.CutPrefix(text, ChatPrefix)
	if !ok {
		return "", false
	}
	idText, action, ok := strings.Cut(rest, ":")
	id, err := uuid.Parse(idText)
	if !ok || err != nil {
		return i18n.T(ctx, "outreach.error.action"), true
	}
	if _, err = s.Act(ctx, user, id, action); err != nil {
		if apperr.Is(err, apperr.ErrConflict) {
			return i18n.T(ctx, "outreach.error.answered"), true
		}
		s.log.Warn("outreach: chat answer failed", "user_id", user.ID, "error", err)
		return i18n.T(ctx, "outreach.error"), true
	}
	return Outcome(ctx, action), true
}

// Outcome is what an answer did, in words.
func Outcome(ctx context.Context, action string) string {
	return i18n.T(ctx, "outreach.done."+action)
}

func chatOptions(ctx context.Context, o Outreach) []messaging.Option {
	actions := o.Actions()
	out := make([]messaging.Option, len(actions))
	for i, a := range actions {
		out[i] = messaging.Option{Label: i18n.T(ctx, "outreach.action."+a), Value: ChatPrefix + o.ID.String() + ":" + a}
	}
	return out
}

// situation is what the coach is told it noticed, for RunOutreach. English:
// the model writes in the person's language from its own instructions.
func situation(o Outreach) string {
	f := o.Facts
	switch o.Trigger {
	case TriggerLowRecovery:
		s := fmt.Sprintf("This morning their recovery is under their usual (%s). Today's planned session is %s.", strings.TrimSpace(f.Reason), f.Session)
		if f.Applied {
			return s + " Because they let you adjust their training, you already cut today's sets to about 60%. Tomorrow is back to the plan. They can undo it."
		}
		return s + " They can take it lighter (about 60% of the sets) or keep the plan."
	default:
		s := fmt.Sprintf("Yesterday's planned session, %s (%s), did not happen.", f.Session, f.Weekday)
		var where []string
		if f.TodayFree {
			where = append(where, "today (a rest day)")
		}
		if f.TomorrowFree {
			where = append(where, "tomorrow (a rest day)")
		}
		if len(where) > 0 {
			s += " It can move to " + strings.Join(where, " or ") + "."
		}
		return s + " Or they can let it go, and the plan picks it up next time."
	}
}

// fallback is the message when the coach could not write one.
func fallback(ctx context.Context, o Outreach) string {
	f := o.Facts
	switch {
	case o.Trigger == TriggerLowRecovery && f.Applied:
		return i18n.Tf(ctx, "outreach.fallback.lighter.applied", f.Session)
	case o.Trigger == TriggerLowRecovery:
		return i18n.Tf(ctx, "outreach.fallback.lighter.offer", f.Session)
	default:
		return i18n.Tf(ctx, "outreach.fallback.missed", f.Session)
	}
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.Month() == b.Month() && a.Day() == b.Day()
}

// CoversRecovery reports a morning the coach already spoke about recovery:
// its message carries the lighter-day answers, so the lighter-day card hides.
func (s *Service) CoversRecovery(ctx context.Context, user users.User) bool {
	o, ok, err := s.Today(ctx, user)
	return err == nil && ok && o.Trigger == TriggerLowRecovery
}
