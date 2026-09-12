package insights

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/users"
)

func TestSweeperSaysNothingToSomebodyWhoNeverChose(t *testing.T) {
	notifier := &stubNotifier{}
	sweeper := NewDigestSweeper(DigestSweeperOptions{
		Accounts: &stubAccounts{users: []users.User{sweeperUser(t)}},
		// A row straight from the migration, and a person who has never
		// opened the setting, must both come out silent.
		Prefs:    &stubPrefs{prefs: notifications.Prefs{}},
		Ledger:   &stubLedger{},
		Digester: &stubDigester{text: "something", worth: true},
		Notify:   notifier,
		Log:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:      func() time.Time { return at(t, "2026-09-14 09:00") },
	})

	if err := sweeper.HandleSweep(context.Background(), nil); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if len(notifier.sent) != 0 {
		t.Errorf("sent %d digests to somebody who never asked", len(notifier.sent))
	}
}
