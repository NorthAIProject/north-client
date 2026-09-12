package insights

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/messaging"
	"github.com/NorthAIProject/north-client/internal/notifications"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/sweep"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

// digestHour is the local hour a digest may first go out. Early enough to be
// read with coffee, late enough not to be the thing that wakes somebody.
const digestHour = 8

// How long a digest stays worth sending, in days after the period opened.
//
// The gate is deliberately loose. The sweep runs hourly, and a worker that was
// down for a whole Monday must not silently skip somebody's week — so a weekly
// digest stays due through Wednesday and a monthly one through the third.
// Sending twice is prevented by the ledger instead, which claims the period
// rather than the day: loose gate, strict ledger.
const (
	weeklyCatchUpDays  = 3
	monthlyCatchUpDays = 3
)

// due reports which window a cadence should cover now, and the period the send
// is booked against.
//
// The window is one of the ranges the selector already offers, so the link at
// the bottom of the digest lands on a page showing exactly what the digest
// described. The period is the calendar unit — the Monday, the first of the
// month — which is what makes the ledger idempotent across the catch-up days.
func due(local time.Time, cadence string) (rangeKey string, period time.Time, ok bool) {
	if local.Hour() < digestHour {
		return "", time.Time{}, false
	}

	switch cadence {
	case notifications.CadenceDaily:
		// Yesterday rather than today: a digest sent at eight in the morning
		// about today would be a report on eight hours, most of them asleep.
		return timerange.KeyYesterday, timerange.StartOfDay(local), true

	case notifications.CadenceWeekly:
		monday := timerange.StartOfWeek(local)
		if daysSince(monday, local) >= weeklyCatchUpDays {
			return "", time.Time{}, false
		}
		return timerange.KeyWeek, monday, true

	case notifications.CadenceMonthly:
		first := timerange.StartOfMonth(local)
		if daysSince(first, local) >= monthlyCatchUpDays {
			return "", time.Time{}, false
		}
		return timerange.KeyMonth, first, true

	default:
		// Off, or a cadence this build no longer understands. Either way the
		// right answer is silence rather than an hourly send.
		return "", time.Time{}, false
	}
}

// daysSince counts calendar days from start to local, so a window crossing a
// clock change is still the number of days somebody lived through.
func daysSince(start, local time.Time) int {
	day := timerange.StartOfDay(local)
	n := 0
	for d := start; d.Before(day); d = d.AddDate(0, 0, 1) {
		n++
	}
	return n
}

// DigestIfWorthSending renders a window and says whether it is worth pushing.
//
// A window with nothing scored in it is not sent. "Nothing logged this week"
// arriving every Monday is the message that teaches somebody to mute the bot,
// and the one week they do log something is the week they no longer read it.
func (s *Service) DigestIfWorthSending(ctx context.Context, user users.User, rg timerange.Range) (string, []byte, bool, error) {
	view, err := s.digestView(ctx, user, rg)
	if err != nil {
		return "", nil, false, err
	}
	if view.Empty || view.Judged == 0 {
		return "", nil, false, nil
	}
	return renderDigest(view, s.siteURL), renderDigestCard(view), true, nil
}

// Digester renders one person's window. *Service is the implementation; the
// interface is here so the sweep can be tested without a database behind it.
type Digester interface {
	DigestIfWorthSending(ctx context.Context, user users.User, rg timerange.Range) (text string, photo []byte, worth bool, err error)
}

// DigestPrefs answers how often this person wants their numbers.
type DigestPrefs interface {
	Get(ctx context.Context, userID uuid.UUID) (notifications.Prefs, error)
}

// DigestLedger books a period so the same one cannot be sent twice.
type DigestLedger interface {
	ClaimDigest(ctx context.Context, userID uuid.UUID, cadence string, period time.Time) (bool, error)
}

// DigestNotifier delivers the message to whatever chats are linked.
type DigestNotifier interface {
	NotifyMessage(ctx context.Context, userID uuid.UUID, msg messaging.OutboundMessage) error
}

type DigestSweeperOptions struct {
	Accounts sweep.Accounts
	Prefs    DigestPrefs
	Ledger   DigestLedger
	Digester Digester
	Notify   DigestNotifier
	Log      *slog.Logger

	// Now defaults to time.Now. Overridable so a test can stand on a Monday.
	Now func() time.Time
}

// DigestSweeper pushes the insights digest on each person's own schedule.
//
// Modelled on the briefing sweeper, with one deliberate difference: no job is
// enqueued per account. The briefing enqueues because generating it is a model
// call worth retrying on its own; a digest is a handful of queries, so the
// sweep sends it inline and a failure simply waits for the next hour.
type DigestSweeper struct {
	accounts sweep.Accounts
	prefs    DigestPrefs
	ledger   DigestLedger
	digester Digester
	notify   DigestNotifier
	log      *slog.Logger
	now      func() time.Time
}

func NewDigestSweeper(opts DigestSweeperOptions) *DigestSweeper {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &DigestSweeper{
		accounts: opts.Accounts,
		prefs:    opts.Prefs,
		ledger:   opts.Ledger,
		digester: opts.Digester,
		notify:   opts.Notify,
		log:      opts.Log,
		now:      now,
	}
}

// HandleSweep is the job handler. Runs hourly; most hours it sends nothing.
func (s *DigestSweeper) HandleSweep(ctx context.Context, _ json.RawMessage) error {
	now := s.now()
	return sweep.Run(ctx, s.accounts, s.log, "stats digest",
		func(ctx context.Context, user users.User) (bool, error) {
			return s.sweepOne(ctx, now, user)
		})
}

func (s *DigestSweeper) sweepOne(ctx context.Context, now time.Time, user users.User) (bool, error) {
	// The worker leaves this nil when no bot token is configured. Nothing
	// below is worth doing if there is nowhere to send the result, and a
	// deployment without Telegram should sweep quietly rather than read
	// everybody's week to throw it away.
	if s.notify == nil {
		return false, nil
	}

	prefs, err := s.prefs.Get(ctx, user.ID)
	if err != nil && !apperr.Is(err, apperr.ErrNotFound) {
		return false, err
	}
	// A person with no prefs row has never opened the settings page, which is
	// not a decision to switch anything off. They get the default cadence.
	if !prefs.WantsDigest() {
		return false, nil
	}

	local := now.In(user.Location())

	key, period, ok := due(local, prefs.Cadence())
	if !ok {
		return false, nil
	}

	// Checked before the period is booked, so a deferral is a deferral rather
	// than a silent skip: the next hourly sweep finds the window still unsent.
	//
	// The reports deliberately do not check this. They differ in kind — a
	// report appears in the app and the Telegram note is incidental, while a
	// digest IS the push, which is exactly what quiet hours exist to govern.
	if prefs.QuietHoursEnabled && prefs.InQuietHours(local) {
		return false, nil
	}

	text, photo, worth, err := s.digester.DigestIfWorthSending(ctx, user, timerange.Parse(key, user.Location()))
	if err != nil {
		return false, err
	}
	if !worth {
		return false, nil
	}

	// Claimed last. Everything above can decline for a reason that may not
	// hold an hour from now, and booking the period before then would spend a
	// send that never happened.
	claimed, err := s.ledger.ClaimDigest(ctx, user.ID, prefs.Cadence(), period)
	if err != nil {
		return false, err
	}
	if !claimed {
		return false, nil
	}

	// The card leads and the words follow, which is how Send orders a message
	// that carries both. The caption stays short because Telegram caps it far
	// below the length of a digest.
	msg := messaging.OutboundMessage{Text: text, Photo: photo}
	if len(photo) > 0 {
		msg.PhotoCaption = timerange.Parse(key, user.Location()).Label
	}

	if err := s.notify.NotifyMessage(ctx, user.ID, msg); err != nil {
		// Logged rather than returned, matching the reports: a chat that could
		// not be reached is not a failure of the sweep, and the period stays
		// claimed so a retry storm cannot follow a flapping transport.
		s.log.Error("stats digest delivery failed",
			slog.String("user_id", user.ID.String()),
			slog.Any("error", err),
		)
	}
	return true, nil
}
