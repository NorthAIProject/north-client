package sleep_test

import (
	"context"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/sleep"
	"github.com/NorthAIProject/north-client/internal/users"
)

func timeNow() time.Time { return time.Now() }

func newService(t *testing.T) (*sleep.Service, users.User) {
	t.Helper()

	pool := testdb.New(t)

	user, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        "fernando@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Fernando Correia",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	return sleep.NewService(sleep.NewRepository(pool)), user
}

func quality(n int) *int { return &n }

// The point of upsert-per-day: correcting this morning's estimate edits the
// night rather than logging a second one.
func TestLoggingTwiceInADayCorrectsRatherThanDuplicates(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	if _, err := svc.LogToday(ctx, user, sleep.Input{DurationMinutes: 400}); err != nil {
		t.Fatalf("first log: %v", err)
	}

	corrected, err := svc.LogToday(ctx, user, sleep.Input{DurationMinutes: 450, Quality: quality(4)})
	if err != nil {
		t.Fatalf("second log: %v", err)
	}
	if corrected.DurationMinutes != 450 {
		t.Errorf("DurationMinutes = %d, want 450", corrected.DurationMinutes)
	}

	recent, err := svc.Recent(ctx, user, 10)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(recent) != 1 {
		t.Fatalf("logged %d nights, want 1 corrected in place", len(recent))
	}
}

func TestTodayReportsAbsenceWithoutError(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	if _, ok, err := svc.Today(ctx, user); err != nil || ok {
		t.Fatalf("Today() on an empty log = ok %v, err %v; want false, nil", ok, err)
	}

	if _, err := svc.LogToday(ctx, user, sleep.Input{DurationMinutes: 465}); err != nil {
		t.Fatalf("log: %v", err)
	}

	log, ok, err := svc.Today(ctx, user)
	if err != nil || !ok {
		t.Fatalf("Today() after logging = ok %v, err %v; want true, nil", ok, err)
	}
	if log.Duration() != "7h 45m" {
		t.Errorf("Duration() = %q, want %q", log.Duration(), "7h 45m")
	}
}

func TestOptionalFieldsRoundTrip(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	// Times are nullable columns with a CHECK constraint, so empty strings
	// have to reach the database as NULL rather than as ''.
	bare, err := svc.LogToday(ctx, user, sleep.Input{DurationMinutes: 400})
	if err != nil {
		t.Fatalf("log without times: %v", err)
	}
	if bare.Bedtime != "" || bare.WakeTime != "" || bare.Quality != nil {
		t.Errorf("unset optionals came back set: %+v", bare)
	}

	full, err := svc.LogToday(ctx, user, sleep.Input{
		DurationMinutes: 465,
		Quality:         quality(4),
		Bedtime:         "23:15",
		WakeTime:        "07:00",
		Notes:           "woke once",
	})
	if err != nil {
		t.Fatalf("log with times: %v", err)
	}
	if full.Bedtime != "23:15" || full.WakeTime != "07:00" {
		t.Errorf("times = %q-%q, want 23:15-07:00", full.Bedtime, full.WakeTime)
	}
	if full.Quality == nil || *full.Quality != 4 {
		t.Errorf("Quality = %v, want 4", full.Quality)
	}
}

// Averaging over nights recorded, not nights elapsed: three logged nights of
// good sleep must not read as a bad week because four were not logged.
func TestTrendAveragesOverRecordedNightsOnly(t *testing.T) {
	svc, user := newService(t)
	ctx := context.Background()

	today := sleep.LocalDate(user, timeNow())
	durations := []int{420, 480, 360}
	for i, minutes := range durations {
		date := today.AddDate(0, 0, -i)
		in := sleep.Input{DurationMinutes: minutes}
		if i == 0 {
			in.Quality = quality(5) // only one night rated
		}
		if _, err := svc.LogFor(ctx, user, date, in); err != nil {
			t.Fatalf("log night %d: %v", i, err)
		}
	}

	trend, err := svc.RecentTrend(ctx, user, 7)
	if err != nil {
		t.Fatalf("trend: %v", err)
	}

	if trend.Nights != 3 {
		t.Errorf("Nights = %d, want 3", trend.Nights)
	}
	if want := float64(420+480+360) / 3; trend.AverageMinutes != want {
		t.Errorf("AverageMinutes = %v, want %v", trend.AverageMinutes, want)
	}
	// Quality averages over the nights that carried a rating, not all three.
	if trend.QualityCount != 1 || trend.AverageQuality != 5 {
		t.Errorf("quality = %v over %d nights, want 5 over 1", trend.AverageQuality, trend.QualityCount)
	}
}

func TestEmptyTrendSaysSoRatherThanReportingZeroHours(t *testing.T) {
	svc, user := newService(t)

	trend, err := svc.RecentTrend(context.Background(), user, 7)
	if err != nil {
		t.Fatalf("trend: %v", err)
	}
	if trend.Summary() != "Sleep: nothing logged yet" {
		t.Errorf("Summary() = %q", trend.Summary())
	}
}

func TestValidationRejectsBadInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   sleep.Input
	}{
		{"no duration", sleep.Input{}},
		{"longer than a day", sleep.Input{DurationMinutes: 1441}},
		{"quality out of range", sleep.Input{DurationMinutes: 400, Quality: quality(6)}},
		{"bedtime not a clock", sleep.Input{DurationMinutes: 400, Bedtime: "11pm"}},
		{"wake time out of range", sleep.Input{DurationMinutes: 400, WakeTime: "25:00"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := sleep.Validate(tt.in); !apperr.Is(err, apperr.ErrValidation) {
				t.Errorf("Validate(%+v) error = %v, want validation error", tt.in, err)
			}
		})
	}
}
