package awards_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func thisMonth() string { return time.Now().UTC().Format("2006-01") }

func TestOwnAwardsPageLeadsWithTheMonthlyCard(t *testing.T) {
	e := newPageEnv(t, true)
	e.api.openMonth(e.owner, thisMonth(), "workouts", 8)
	rec := e.do(http.MethodGet, "/app/awards", e.owner, nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	for _, want := range []string{`id="monthly-challenge"`, "challenge", "Log 8 workouts in", "0 of 8 workouts", `data-tui-progress-indicator`} {
		if !strings.Contains(body, want) {
			t.Errorf("own page lacks %q", want)
		}
	}
	if strings.Contains(strings.ToLower(body), "missed") {
		t.Error("the page talks about a missed month")
	}
}

func TestOthersAwardsPageHasNoMonthlyCardButShowsTheirMedals(t *testing.T) {
	e := newPageEnv(t, true)
	e.api.meetMonth(e.owner, "2026-09", "workouts", 12, time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC))
	body := e.do(http.MethodGet, "/app/u/pat/awards", e.other, nil).Body.String()
	if strings.Contains(body, `id="monthly-challenge"`) {
		t.Error("another person's awards page shows a challenge card")
	}
	if !strings.Contains(body, `data-badge="monthly-2026-09"`) || !strings.Contains(body, ">SEP<") || !strings.Contains(body, ">12<") {
		t.Errorf("their earned list lacks the September coin: %s", body)
	}
}

func TestUnlockDialogShowsAMonthlyMedalAndItsSeenIDPosts(t *testing.T) {
	e := newPageEnv(t, true)
	e.api.meetMonth(e.owner, "2026-09", "workouts", 12, time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC))
	rec := e.do(http.MethodGet, "/app/awards/unseen", e.owner, nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	for _, want := range []string{`value="monthly-2026-09"`, ">SEP<", ">12<", "September 2026 challenge", "#D4A017"} {
		if !strings.Contains(body, want) {
			t.Errorf("unlock lacks %q", want)
		}
	}
	rec = e.do(http.MethodPost, "/app/awards/seen", e.owner, url.Values{"badge": {"monthly-2026-09"}})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("seen status %d", rec.Code)
	}
	if rec := e.do(http.MethodGet, "/app/awards/unseen", e.owner, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("still unseen: %d", rec.Code)
	}
}
