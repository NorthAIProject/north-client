package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/biometrics"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	"github.com/NorthAIProject/north-client/internal/users"
)

func TestLogActivityIsAWriteThatNeedsTheUserRecord(t *testing.T) {
	t.Parallel()

	activitySvc := activity.NewService(activity.NewRepository(nil), nil)
	userSvc := users.NewService(users.NewRepository(nil))

	with := Build(Services{Activity: activitySvc, Users: userSvc})
	if with.IsReadOnly("log_activity") {
		t.Error("log_activity reports read-only; it writes")
	}
	if with.IsIdempotent("log_activity") {
		t.Error("log_activity reports idempotent; two runs are two runs")
	}

	without := Build(Services{Activity: activitySvc})
	for _, tool := range without.Tools() {
		if tool.Name == "log_activity" {
			t.Error("log_activity was published without a users service; a spoken start time has no zone to be read in")
		}
	}
}

func TestMatchActivityUsesThePaceToPickTheRun(t *testing.T) {
	t.Parallel()

	met, err := matchActivity("run", 5, 30*time.Minute) // 10 km/h
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if met.Code != "running_9_8kmh" {
		t.Errorf("Code = %q, want running_9_8kmh", met.Code)
	}

	if _, err := matchActivity("run", 0, 30*time.Minute); err == nil || !strings.Contains(err.Error(), "Running (8 km/h)") {
		t.Errorf("a run without a distance should list the running entries, got %v", err)
	}
	if _, err := matchActivity("levitating", 0, 30*time.Minute); err == nil {
		t.Error("an unknown activity should be refused")
	}
}

func TestParseLocalTimeReadsTheUsersClock(t *testing.T) {
	t.Parallel()

	lisbon, _ := time.LoadLocation("Europe/Lisbon")

	got, err := parseLocalTime("2026-09-10 07:30", lisbon)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := time.Date(2026, 9, 10, 7, 30, 0, 0, lisbon)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}

	if got, err := parseLocalTime("  ", lisbon); err != nil || !got.IsZero() {
		t.Errorf("empty should mean just now, got %v, %v", got, err)
	}
	if _, err := parseLocalTime("yesterday-ish", lisbon); err == nil {
		t.Error("nonsense should be refused")
	}
}

// End to end: a spoken run becomes a completed session with its distance.
func TestLogActivityWritesARealRun(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()

	userSvc := users.NewService(users.NewRepository(pool))
	biometricSvc := biometrics.NewService(biometrics.NewRepository(pool))
	activitySvc := activity.NewService(activity.NewRepository(pool), biometricSvc)

	user, err := userSvc.Register(ctx, users.Registration{
		Email:        "runner@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Fernando Correia",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, baseErr := biometricSvc.Record(ctx, user.ID, biometrics.Input{
		WeightKg: 80, HeightCm: 180,
		DateOfBirth: time.Now().AddDate(-30, 0, 0),
		Sex:         biometrics.SexMale,
	}); baseErr != nil {
		t.Fatalf("baseline: %v", baseErr)
	}

	r := Build(Services{Activity: activitySvc, Users: userSvc})

	raw, _ := json.Marshal(map[string]any{
		"activity": "went for a run", "minutes": 30, "distance_km": 5, "started_at": "",
	})
	result := r.Invoke(ctx, user.ID, ai.ToolCall{Name: "log_activity", Arguments: raw})
	if result.IsError {
		t.Fatalf("log_activity: %s", result.Content)
	}
	for _, want := range []string{"Running (9.8 km/h)", "30 min", "5.00 km", "6:00 /km", "392 kcal"} {
		if !strings.Contains(result.Content, want) {
			t.Errorf("confirmation %q lacks %q", result.Content, want)
		}
	}

	sessions, err := activitySvc.List(ctx, user.ID, 10)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions = %d (err %v), want 1", len(sessions), err)
	}
	s := sessions[0]
	if s.ActivityCode != "running_9_8kmh" || s.Status != activity.StatusCompleted {
		t.Errorf("stored %s/%s", s.ActivityCode, s.Status)
	}
	if s.DistanceM == nil || *s.DistanceM != 5000 {
		t.Errorf("DistanceM = %v, want 5000", s.DistanceM)
	}

	// An ambiguous activity is refused with the choices, not written.
	raw, _ = json.Marshal(map[string]any{"activity": "walk", "minutes": 40, "distance_km": 0, "started_at": ""})
	result = r.Invoke(ctx, user.ID, ai.ToolCall{Name: "log_activity", Arguments: raw})
	if !result.IsError || !strings.Contains(result.Content, "Walking (brisk pace)") {
		t.Errorf("ambiguous walk: isError=%t %q", result.IsError, result.Content)
	}
}
