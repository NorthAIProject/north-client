package messaging_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/NorthAIProject/north-client/internal/ai/fake"
	"github.com/NorthAIProject/north-client/internal/messaging"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
)

func TestStatsAnswersWithoutTheModelOrTheQuota(t *testing.T) {
	// Like every other command: a digest is read from the database, so it
	// must not cost a coach message or reach a paid provider.
	client := fake.Text("this should never be generated")
	stats := &stubStats{text: "Last 7 days\n2 of 3 on track."}
	quotas := &stubQuotas{allowed: true}
	h := newHarness(t, client, harnessOptions{quotas: quotas, stats: stats})
	h.link(t, chat)

	out := h.send(t, chat, "/stats")

	if len(client.Calls()) != 0 {
		t.Error("/stats reached the model")
	}
	if quotas.consumed != 0 {
		t.Errorf("/stats spent %d from the budget", quotas.consumed)
	}
	if !strings.Contains(out.Text, "on track") {
		t.Errorf("reply did not carry the digest: %q", out.Text)
	}
}

func TestStatsDefaultsToTheDayWithNoArgument(t *testing.T) {
	stats := &stubStats{text: "today"}
	h := newHarness(t, fake.Text("unused"), harnessOptions{stats: stats})
	h.link(t, chat)

	h.send(t, chat, "/stats")

	if stats.gotKey != timerange.KeyToday {
		t.Errorf("asked for %q, want %q", stats.gotKey, timerange.KeyToday)
	}
}

func TestStatsReadsItsArgument(t *testing.T) {
	for _, arg := range []string{"week", "month", "quarter", "year"} {
		t.Run(arg, func(t *testing.T) {
			stats := &stubStats{text: arg}
			h := newHarness(t, fake.Text("unused"), harnessOptions{stats: stats})
			h.link(t, chat)

			h.send(t, chat, "/stats "+arg)

			if stats.gotKey != arg {
				t.Errorf("asked for %q, want %q", stats.gotKey, arg)
			}
		})
	}
}

func TestStatsTreatsNonsenseAsToday(t *testing.T) {
	// A typed argument must not be able to produce an error message. The
	// range parser already takes this position for a hand-edited URL.
	stats := &stubStats{text: "today"}
	h := newHarness(t, fake.Text("unused"), harnessOptions{stats: stats})
	h.link(t, chat)

	out := h.send(t, chat, "/stats banana")

	if stats.gotKey != timerange.KeyToday {
		t.Errorf("asked for %q, want %q", stats.gotKey, timerange.KeyToday)
	}
	if strings.TrimSpace(out.Text) == "" {
		t.Error("said nothing")
	}
}

func TestStatsIsUppercaseTolerant(t *testing.T) {
	stats := &stubStats{text: "week"}
	h := newHarness(t, fake.Text("unused"), harnessOptions{stats: stats})
	h.link(t, chat)

	h.send(t, chat, "/stats Week")

	if stats.gotKey != timerange.KeyWeek {
		t.Errorf("asked for %q, want %q", stats.gotKey, timerange.KeyWeek)
	}
}

func TestStatsWithoutTheServicePointsAtTheWebApp(t *testing.T) {
	// A build with no insights service wired must answer, not crash and not
	// hand the command to the coach as prose.
	h := newHarness(t, fake.Text("unused"), harnessOptions{})
	h.link(t, chat)

	out := h.send(t, chat, "/stats")

	if strings.TrimSpace(out.Text) == "" {
		t.Fatal("said nothing with no stats service")
	}
}

func TestStatsSurfacesAFailureAsWords(t *testing.T) {
	stats := &stubStats{err: errors.New("database is down")}
	h := newHarness(t, fake.Text("unused"), harnessOptions{stats: stats})
	h.link(t, chat)

	out := h.send(t, chat, "/stats")

	if strings.Contains(out.Text, "database is down") {
		t.Errorf("leaked the internal error to the reader: %q", out.Text)
	}
	if strings.TrimSpace(out.Text) == "" {
		t.Error("said nothing when the digest failed")
	}
}

func messagingCommands() []messaging.Command { return messaging.Commands() }

func TestStatsIsOfferedInTheCommandMenu(t *testing.T) {
	// The menu is registered from this list, so a command missing here is a
	// command nobody discovers.
	var found bool
	for _, c := range messagingCommands() {
		if c.Name == "stats" {
			found = true
		}
	}
	if !found {
		t.Error("/stats is not in the command menu")
	}
}

func TestStatsSendsTheCardWithTheWords(t *testing.T) {
	stats := &stubStats{text: "Last 7 days\n2 of 3 on track.", photo: []byte("\x89PNG fake")}
	h := newHarness(t, fake.Text("unused"), harnessOptions{stats: stats})
	h.link(t, chat)

	out := h.send(t, chat, "/stats week")

	if len(out.Photo) == 0 {
		t.Error("the card did not reach the reply")
	}
	if out.PhotoCaption == "" {
		t.Error("the card was sent with no caption")
	}
	if !strings.Contains(out.Text, "on track") {
		t.Errorf("the words were lost: %q", out.Text)
	}
}

func TestStatsWithNoCardStillAnswers(t *testing.T) {
	stats := &stubStats{text: "Last 7 days"}
	h := newHarness(t, fake.Text("unused"), harnessOptions{stats: stats})
	h.link(t, chat)

	out := h.send(t, chat, "/stats")

	if len(out.Photo) != 0 {
		t.Error("invented a card")
	}
	if out.PhotoCaption != "" {
		t.Error("captioned a card that does not exist")
	}
	if strings.TrimSpace(out.Text) == "" {
		t.Error("said nothing")
	}
}
