package awards_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/users"
)

var (
	october  = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	november = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
)

// meetMonth stores a challenge and its met row, as the engine leaves them.
func (e *apiEnv) meetMonth(u users.User, key, metric string, target int, at time.Time) {
	e.t.Helper()
	ctx := context.Background()
	if _, err := e.db.Exec(ctx, `INSERT INTO monthly_challenges (user_id, month, metric, target) VALUES ($1, ($2 || '-01')::date, $3, $4)
		ON CONFLICT DO NOTHING`, u.ID, key, metric, target); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.db.Exec(ctx, `INSERT INTO achievements (user_id, category, kind, title, detail, occurred_at, source_key, private)
		VALUES ($1, 'awards', 'monthly_challenge_met', 'Challenge met', '', $2, $3, false)`, u.ID, at, key); err != nil {
		e.t.Fatal(err)
	}
}

// openMonth stores a challenge that was never met.
func (e *apiEnv) openMonth(u users.User, key, metric string, target int) {
	e.t.Helper()
	if _, err := e.db.Exec(context.Background(), `INSERT INTO monthly_challenges (user_id, month, metric, target) VALUES ($1, ($2 || '-01')::date, $3, $4)`,
		u.ID, key, metric, target); err != nil {
		e.t.Fatal(err)
	}
}

type awardJSON struct {
	ID          string `json:"id"`
	Area        string `json:"area"`
	Name        string `json:"name"`
	Description string `json:"description"`
	EarnedAt    string `json:"earnedAt"`
	Monthly     *struct {
		Month, Metric string
		Target        int
	} `json:"monthly"`
}

type challengeJSON struct {
	Month    string `json:"month"`
	Metric   string `json:"metric"`
	Target   int    `json:"target"`
	Progress int    `json:"progress"`
	Met      bool   `json:"met"`
	Title    string `json:"title"`
	Goal     string `json:"goal"`
}

type awardsJSON struct {
	Earned           []awardJSON    `json:"earned"`
	Locked           []awardJSON    `json:"locked"`
	MonthlyChallenge *challengeJSON `json:"monthlyChallenge"`
}

func (e *apiEnv) awardsList() awardsJSON {
	e.t.Helper()
	rec := e.do(http.MethodGet, "/awards", e.user)
	if rec.Code != http.StatusOK {
		e.t.Fatalf("GET /awards = %d %s", rec.Code, rec.Body)
	}
	var v awardsJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		e.t.Fatal(err)
	}
	return v
}

func (e *apiEnv) unseenIDs() []string {
	e.t.Helper()
	var v struct{ Awards []awardJSON }
	if err := json.Unmarshal(e.do(http.MethodGet, "/awards/unseen", e.user).Body.Bytes(), &v); err != nil {
		e.t.Fatal(err)
	}
	ids := []string{}
	for _, a := range v.Awards {
		ids = append(ids, a.ID)
	}
	return ids
}

func TestMonthlyMedalInAwardsAndUnseen(t *testing.T) {
	e := newAPIEnv(t, "monthly-medal@north.test")
	e.svc.SetClock(func() time.Time { return october })
	e.svc.Grant(context.Background(), e.user.ID, october.Add(-48*time.Hour), "workouts_1")
	e.meetMonth(e.user, "2026-10", "workouts", 5, october.Add(-time.Hour))

	v := e.awardsList()
	if len(v.Earned) != 2 || v.Earned[0].ID != "workouts_1" || v.Earned[1].ID != "monthly-2026-10" {
		t.Fatalf("earned = %+v, want the older badge first, then the monthly medal (oldest first)", v.Earned)
	}
	m := v.Earned[1]
	if m.Area != "monthly" || m.Monthly == nil || m.Monthly.Month != "2026-10" || m.Monthly.Metric != "workouts" || m.Monthly.Target != 5 ||
		m.EarnedAt == "" || m.Name != "October 2026 challenge" || m.Description != "Logged 5 workouts in October" {
		t.Fatalf("medal = %+v", m)
	}
	if len(v.Locked) != 30 {
		t.Fatalf("locked = %d, the catalog must stay 31 badges", len(v.Locked))
	}
	if v.MonthlyChallenge == nil || !v.MonthlyChallenge.Met || v.MonthlyChallenge.Month != "2026-10" {
		t.Fatalf("challenge = %+v", v.MonthlyChallenge)
	}

	if got := e.unseenIDs(); len(got) != 2 || got[0] != "workouts_1" || got[1] != "monthly-2026-10" {
		t.Fatalf("unseen = %v, want both, oldest first", got)
	}
	if c := e.do(http.MethodPost, "/awards/monthly-2026-10/seen", e.user).Code; c != http.StatusNoContent {
		t.Fatalf("seen = %d", c)
	}
	if got := e.unseenIDs(); len(got) != 1 || got[0] != "workouts_1" {
		t.Fatalf("unseen after seen = %v", got)
	}
	if c := e.do(http.MethodPost, "/awards/monthly-2026-10/seen", e.user).Code; c != http.StatusNoContent {
		t.Fatalf("repeat seen = %d", c)
	}
	var celebrated bool
	if err := e.db.QueryRow(context.Background(), `SELECT celebrated_at IS NOT NULL FROM achievements WHERE user_id=$1 AND kind='monthly_challenge_met'`, e.user.ID).Scan(&celebrated); err != nil || !celebrated {
		t.Fatalf("celebrated_at not set on the monthly row: %v %v", celebrated, err)
	}
}

func TestSeenMonthlyOnlyOwn(t *testing.T) {
	e := newAPIEnv(t, "monthly-seen@north.test")
	other, err := users.NewService(users.NewRepository(e.db)).Register(context.Background(), users.Registration{
		Email: "monthly-seen-other@north.test", PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly", DisplayName: "Sam", Timezone: "UTC",
	})
	if err != nil {
		t.Fatal(err)
	}
	e.meetMonth(e.user, "2026-09", "checkin_days", 8, october)
	e.meetMonth(other, "2026-08", "checkin_days", 8, october)

	for path, want := range map[string]int{
		"/awards/monthly-2026-09/seen":  http.StatusNoContent,
		"/awards/monthly-2026-08/seen":  http.StatusNotFound, // somebody else's month
		"/awards/monthly-2026-07/seen":  http.StatusNotFound, // a month never met
		"/awards/monthly-2026-13/seen":  http.StatusNotFound, // malformed
		"/awards/monthly-nope/seen":     http.StatusNotFound,
		"/awards/monthly-/seen":         http.StatusNotFound,
		"/awards/monthly-2026-9/seen":   http.StatusNotFound,
		"/awards/monthly-2026-09x/seen": http.StatusNotFound,
	} {
		if got := e.do(http.MethodPost, path, e.user).Code; got != want {
			t.Errorf("POST %s = %d, want %d", path, got, want)
		}
	}
	var celebrated bool
	if err := e.db.QueryRow(context.Background(), `SELECT celebrated_at IS NOT NULL FROM achievements WHERE user_id=$1 AND source_key='2026-08'`, other.ID).Scan(&celebrated); err != nil || celebrated {
		t.Fatalf("another user's row was marked: %v %v", celebrated, err)
	}
}

func TestMissedMonthIsNeverShown(t *testing.T) {
	e := newAPIEnv(t, "monthly-missed@north.test")
	e.openMonth(e.user, "2026-10", "checkin_days", 8) // never met
	e.meetMonth(e.user, "2026-09", "workouts", 4, october.AddDate(0, -1, 0))
	e.svc.SetClock(func() time.Time { return november })

	v := e.awardsList()
	if v.MonthlyChallenge == nil || v.MonthlyChallenge.Month != "2026-11" {
		t.Fatalf("challenge = %+v, want only November's", v.MonthlyChallenge)
	}
	raw := e.do(http.MethodGet, "/awards", e.user).Body.String()
	for _, a := range v.Earned {
		if a.ID == "monthly-2026-10" {
			t.Fatalf("a missed month is listed as a medal: %s", raw)
		}
	}
	if len(v.Earned) != 1 || v.Earned[0].ID != "monthly-2026-09" {
		t.Fatalf("earned = %+v, want the met September medal to live on", v.Earned)
	}
	if got := e.unseenIDs(); len(got) != 1 || got[0] != "monthly-2026-09" {
		t.Fatalf("unseen = %v", got)
	}
}

func TestMonthlyChallengeIsLazilyCreated(t *testing.T) {
	e := newAPIEnv(t, "monthly-lazy@north.test")
	e.svc.SetClock(func() time.Time { return october })
	v := e.awardsList()
	c := v.MonthlyChallenge
	if c == nil || c.Month != "2026-10" || c.Target < 1 || c.Progress != 0 || c.Met || c.Title != "October challenge" || c.Goal == "" {
		t.Fatalf("challenge = %+v", c)
	}
	var n int
	if err := e.db.QueryRow(context.Background(), `SELECT count(*) FROM monthly_challenges WHERE user_id=$1`, e.user.ID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows = %d %v", n, err)
	}
}
