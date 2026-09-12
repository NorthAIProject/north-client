package insights

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/messaging"
	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

type stubAccounts struct{ users []users.User }

func (s *stubAccounts) ListOnboarded(_ context.Context, after uuid.UUID, _ int) ([]users.User, error) {
	if after != uuid.Nil {
		return nil, nil
	}
	return s.users, nil
}

type stubPrefs struct {
	prefs notifications.Prefs
	err   error
}

func (s *stubPrefs) Get(context.Context, uuid.UUID) (notifications.Prefs, error) {
	return s.prefs, s.err
}

type stubLedger struct {
	claimed  map[string]bool
	attempts int
}

func (s *stubLedger) ClaimDigest(_ context.Context, userID uuid.UUID, cadence string, period time.Time) (bool, error) {
	s.attempts++
	if s.claimed == nil {
		s.claimed = map[string]bool{}
	}
	key := userID.String() + cadence + period.Format("2006-01-02")
	if s.claimed[key] {
		return false, nil
	}
	s.claimed[key] = true
	return true, nil
}

type stubDigester struct {
	text  string
	photo []byte
	worth bool
	err   error
	keys  []string
}

func (s *stubDigester) DigestIfWorthSending(_ context.Context, _ users.User, rg timerange.Range) (string, []byte, bool, error) {
	s.keys = append(s.keys, rg.Key)
	return s.text, s.photo, s.worth, s.err
}

type stubNotifier struct {
	sent []messaging.OutboundMessage
	err  error
}

func (s *stubNotifier) NotifyMessage(_ context.Context, _ uuid.UUID, msg messaging.OutboundMessage) error {
	s.sent = append(s.sent, msg)
	return s.err
}

func sweeperUser(t *testing.T) users.User {
	t.Helper()
	return users.User{ID: uuid.New(), Timezone: "Europe/Lisbon"}
}

type sweeperKit struct {
	sweeper  *DigestSweeper
	ledger   *stubLedger
	notifier *stubNotifier
	digester *stubDigester
}

func newSweeper(t *testing.T, cadence string, now time.Time, opts ...func(*stubPrefs)) sweeperKit {
	t.Helper()
	prefs := &stubPrefs{prefs: notifications.Prefs{StatsDigestCadence: cadence}}
	for _, o := range opts {
		o(prefs)
	}
	ledger := &stubLedger{}
	notifier := &stubNotifier{}
	digester := &stubDigester{text: "Last 7 days\n2 of 3 on track.", worth: true}

	return sweeperKit{
		sweeper: NewDigestSweeper(DigestSweeperOptions{
			Accounts: &stubAccounts{users: []users.User{sweeperUser(t)}},
			Prefs:    prefs,
			Ledger:   ledger,
			Digester: digester,
			Notify:   notifier,
			Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
			Now:      func() time.Time { return now },
		}),
		ledger:   ledger,
		notifier: notifier,
		digester: digester,
	}
}

// 2026-09-14 is a Monday.
func monday(t *testing.T, spec string) time.Time { return at(t, spec) }

func TestSweeperSendsAWeeklyDigestOnMonday(t *testing.T) {
	kit := newSweeper(t, notifications.CadenceWeekly, monday(t, "2026-09-14 09:00"))

	if err := kit.sweeper.HandleSweep(context.Background(), nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(kit.notifier.sent) != 1 {
		t.Fatalf("sent %d digests, want 1", len(kit.notifier.sent))
	}
	if kit.digester.keys[0] != timerange.KeyWeek {
		t.Errorf("asked for %q, want %q", kit.digester.keys[0], timerange.KeyWeek)
	}
}

func TestSweeperSendsOnlyOncePerPeriod(t *testing.T) {
	// The gate is loose so a downed worker catches up; the ledger is what
	// stops the catch-up becoming a second send.
	kit := newSweeper(t, notifications.CadenceWeekly, monday(t, "2026-09-14 09:00"))
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := kit.sweeper.HandleSweep(ctx, nil); err != nil {
			t.Fatalf("sweep %d: %v", i, err)
		}
	}

	if len(kit.notifier.sent) != 1 {
		t.Errorf("sent %d digests across three sweeps, want 1", len(kit.notifier.sent))
	}
}

func TestSweeperSendsNothingWhenSwitchedOff(t *testing.T) {
	kit := newSweeper(t, notifications.CadenceOff, monday(t, "2026-09-14 09:00"))

	if err := kit.sweeper.HandleSweep(context.Background(), nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(kit.notifier.sent) != 0 {
		t.Errorf("sent %d digests with the digest off", len(kit.notifier.sent))
	}
}

func TestSweeperWaitsOutQuietHours(t *testing.T) {
	// Unlike the reports, a digest IS the push rather than a note beside a
	// page — so the window that exists to govern pushes governs this one.
	kit := newSweeper(t, notifications.CadenceWeekly, monday(t, "2026-09-14 09:00"),
		func(p *stubPrefs) {
			p.prefs.QuietHoursEnabled = true
			p.prefs.QuietStart = "08:00"
			p.prefs.QuietEnd = "12:00"
		})

	if err := kit.sweeper.HandleSweep(context.Background(), nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(kit.notifier.sent) != 0 {
		t.Errorf("sent %d digests during quiet hours", len(kit.notifier.sent))
	}
	if kit.ledger.attempts != 0 {
		t.Error("claimed the period while deferring, so the catch-up is lost")
	}
}

func TestSweeperDoesNotClaimAWindowItWillNotSend(t *testing.T) {
	// A week with nothing in it is skipped without booking the period, so a
	// later sweep in the same week can still send once data arrives.
	kit := newSweeper(t, notifications.CadenceWeekly, monday(t, "2026-09-14 09:00"))
	kit.digester.worth = false

	if err := kit.sweeper.HandleSweep(context.Background(), nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(kit.notifier.sent) != 0 {
		t.Errorf("sent %d digests for an empty window", len(kit.notifier.sent))
	}
	if kit.ledger.attempts != 0 {
		t.Error("booked a period it did not send")
	}
}

func TestSweeperKeepsGoingWhenOnePersonFails(t *testing.T) {
	kit := newSweeper(t, notifications.CadenceWeekly, monday(t, "2026-09-14 09:00"))
	kit.digester.err = errors.New("one broken row")

	if err := kit.sweeper.HandleSweep(context.Background(), nil); err != nil {
		t.Fatalf("one failing account stopped the whole sweep: %v", err)
	}
}

func TestSweeperIsNotDueBeforeTheMorning(t *testing.T) {
	kit := newSweeper(t, notifications.CadenceDaily, monday(t, "2026-09-14 06:00"))

	if err := kit.sweeper.HandleSweep(context.Background(), nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(kit.notifier.sent) != 0 {
		t.Error("sent a digest before the morning hour")
	}
}

func TestSweeperWithNoTransportSendsNothingAndDoesNotPanic(t *testing.T) {
	// The worker leaves the notifier nil when no bot token is configured. A
	// deployment without Telegram must sweep quietly, not crash hourly.
	sweeper := NewDigestSweeper(DigestSweeperOptions{
		Accounts: &stubAccounts{users: []users.User{sweeperUser(t)}},
		Prefs:    &stubPrefs{prefs: notifications.Prefs{StatsDigestCadence: notifications.CadenceWeekly}},
		Ledger:   &stubLedger{},
		Digester: &stubDigester{text: "something", worth: true},
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:      func() time.Time { return at(t, "2026-09-14 09:00") },
	})

	if err := sweeper.HandleSweep(context.Background(), nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}
}

func TestSweeperSendsTheCardWithTheWords(t *testing.T) {
	// The picture is the point of the phase; a fan-out that dropped it would
	// still pass every other test here.
	kit := newSweeper(t, notifications.CadenceWeekly, monday(t, "2026-09-14 09:00"))
	kit.digester.photo = []byte("\x89PNG fake")

	if err := kit.sweeper.HandleSweep(context.Background(), nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(kit.notifier.sent) != 1 {
		t.Fatalf("sent %d messages, want 1", len(kit.notifier.sent))
	}
	sent := kit.notifier.sent[0]
	if len(sent.Photo) == 0 {
		t.Error("the card did not reach the transport")
	}
	if sent.PhotoCaption == "" {
		t.Error("the card was sent with no caption")
	}
	if sent.Text == "" {
		t.Error("the words were lost along the way")
	}
}

func TestSweeperSendsWordsAloneWhenTheCardWillNotDraw(t *testing.T) {
	// A card that failed to render must not cost the digest.
	kit := newSweeper(t, notifications.CadenceWeekly, monday(t, "2026-09-14 09:00"))
	kit.digester.photo = nil

	if err := kit.sweeper.HandleSweep(context.Background(), nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(kit.notifier.sent) != 1 || kit.notifier.sent[0].Text == "" {
		t.Errorf("sent %+v, want the words on their own", kit.notifier.sent)
	}
	if kit.notifier.sent[0].PhotoCaption != "" {
		t.Error("captioned a card that does not exist")
	}
}
