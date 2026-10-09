package nudges_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/medications"
	"github.com/NorthAIProject/north-client/internal/notifications"
	"github.com/NorthAIProject/north-client/internal/nudges"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
)

func openMedicationReminders(t *testing.T, svc *nudges.Service, userID uuid.UUID) []nudges.Nudge {
	t.Helper()
	list, err := svc.ListOpen(context.Background(), userID, 50)
	if err != nil {
		t.Fatal(err)
	}
	var out []nudges.Nudge
	for _, n := range list {
		if n.Kind == nudges.KindMedicationReminder {
			out = append(out, n)
		}
	}
	return out
}

// A dose reminder is raised once its time comes, once per slot however many
// sweeps run, not at all once the dose is logged — and in quiet hours too,
// because the person set it like an alarm.
func TestMedicationReminderIsAnAlarm(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := mustOnboard(t, pool, seedUser(t, pool, "meds-due@north.test"), time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))
	savePrefs(t, pool, user.ID, notifications.Input{
		NudgeMissedCheckIn: true, QuietHoursEnabled: true, QuietStart: "22:00", QuietEnd: "07:00",
	})

	night := time.Date(2026, 9, 2, 22, 30, 0, 0, time.UTC)
	meds := medications.NewService(medications.NewRepository(pool)).WithClock(func() time.Time { return night })
	med, err := meds.Add(ctx, user, medications.Input{Name: "Melatonin", Dose: "3 mg", Times: []string{"22:15"}, Remind: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := meds.Add(ctx, user, medications.Input{Name: "Quiet pill", Times: []string{"22:15"}, Remind: false}); err != nil {
		t.Fatal(err)
	}

	before := prefsService(pool, time.Date(2026, 9, 2, 22, 0, 0, 0, time.UTC)).WithMedications(meds)
	if _, err := before.Evaluate(ctx, user); err != nil {
		t.Fatal(err)
	}
	if got := openMedicationReminders(t, before, user.ID); len(got) != 0 {
		t.Fatalf("raised before 22:15: %+v", got)
	}

	svc := prefsService(pool, night).WithMedications(meds)
	for range 2 {
		if _, err := svc.Evaluate(ctx, user); err != nil {
			t.Fatal(err)
		}
	}
	got := openMedicationReminders(t, svc, user.ID)
	want := "2026-09-02:" + med.ID.String() + ":22:15"
	if len(got) != 1 || got[0].DedupeKey != want || got[0].Title != "Melatonin 3 mg" {
		t.Fatalf("open = %+v, want one reminder keyed %s", got, want)
	}
}

func TestMedicationReminderSkipsALoggedDose(t *testing.T) {
	pool := testdb.New(t)
	ctx := context.Background()
	user := mustOnboard(t, pool, seedUser(t, pool, "meds-logged@north.test"), time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC))

	now := time.Date(2026, 9, 2, 8, 30, 0, 0, time.UTC)
	meds := medications.NewService(medications.NewRepository(pool)).WithClock(func() time.Time { return now })
	med, err := meds.Add(ctx, user, medications.Input{Name: "Metformin", Dose: "500 mg", Times: []string{"08:00"}, Remind: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := meds.LogDose(ctx, user, med.ID, "", nil); err != nil {
		t.Fatal(err)
	}

	svc := evalService(pool, now).WithMedications(meds)
	if _, err := svc.Evaluate(ctx, user); err != nil {
		t.Fatal(err)
	}
	if got := openMedicationReminders(t, svc, user.ID); len(got) != 0 {
		t.Fatalf("reminded a dose already taken: %+v", got)
	}
}
