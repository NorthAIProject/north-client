package medications_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/NorthAIProject/north-client/internal/coach"
	"github.com/NorthAIProject/north-client/internal/medications"
	"github.com/NorthAIProject/north-client/internal/medications/medication"
	"github.com/NorthAIProject/north-client/internal/shared/database/testdb"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// 09:30 in Lisbon on Thursday 8 October 2026 (UTC+1).
var morning = time.Date(2026, 10, 8, 8, 30, 0, 0, time.UTC)

func newService(t *testing.T, now time.Time) (*medications.Service, users.User) {
	t.Helper()

	pool := testdb.New(t)

	user, err := users.NewService(users.NewRepository(pool)).Register(context.Background(), users.Registration{
		Email:        "meds@north.test",
		PasswordHash: "$2a$12$notarealhashbutthatisfineheretestonly",
		DisplayName:  "Fernando Correia",
		Timezone:     "Europe/Lisbon",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	svc := medications.NewService(medications.NewRepository(pool)).WithClock(func() time.Time { return now })
	return svc, user
}

func addMetformin(t *testing.T, svc *medications.Service, user users.User) medications.Medication {
	t.Helper()
	med, err := svc.Add(context.Background(), user, medications.Input{
		Name: "Metformin", Dose: "500 mg", Times: []string{"20:00", "08:00"}, Remind: true,
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	return med
}

func TestAddRejectsASecondActiveMedicationOfTheSameName(t *testing.T) {
	svc, user := newService(t, morning)
	ctx := context.Background()
	addMetformin(t, svc, user)

	_, err := svc.Add(ctx, user, medications.Input{Name: "metformin", Dose: "850 mg", Times: []string{"08:00"}})
	if !apperr.Is(err, apperr.ErrValidation) {
		t.Fatalf("duplicate name: err = %v, want a validation error", err)
	}
}

// A dose logged without a time answers the nearest open slot, and logging
// the same slot again changes its answer rather than adding a second row.
func TestLogDosePicksTheSlotAndUpserts(t *testing.T) {
	svc, user := newService(t, morning)
	ctx := context.Background()
	med := addMetformin(t, svc, user)

	dose, err := svc.LogDose(ctx, user, med.ID, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if dose.Slot == nil || *dose.Slot != "08:00" || dose.Status != medication.StatusTaken {
		t.Fatalf("dose = %+v, want 08:00 taken", dose)
	}

	slot := "08:00"
	if _, err := svc.LogDose(ctx, user, med.ID, medication.StatusSkipped, &slot); err != nil {
		t.Fatal(err)
	}
	doses, err := svc.TodayDoses(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if len(doses) != 1 || doses[0].Status != medication.StatusSkipped || doses[0].Name != "Metformin" {
		t.Fatalf("doses = %+v, want the one 08:00 row now skipped", doses)
	}

	// The next log without a time is the evening dose.
	if dose, err = svc.LogDose(ctx, user, med.ID, "", nil); err != nil || *dose.Slot != "20:00" {
		t.Fatalf("second log = %+v, %v; want 20:00", dose, err)
	}
	if _, err = svc.LogDose(ctx, user, med.ID, "", nil); !apperr.Is(err, apperr.ErrValidation) {
		t.Errorf("third log with every slot answered: err = %v, want a validation error", err)
	}

	bad := "13:00"
	if _, err = svc.LogDose(ctx, user, med.ID, "", &bad); !apperr.Is(err, apperr.ErrValidation) {
		t.Errorf("unscheduled time: err = %v", err)
	}
}

func TestAsNeededDosesEachKeepTheirOwnRow(t *testing.T) {
	svc, user := newService(t, morning)
	ctx := context.Background()
	inhaler, err := svc.Add(ctx, user, medications.Input{Name: "Salbutamol", Dose: "1 puff", Remind: true})
	if err != nil {
		t.Fatal(err)
	}

	for range 2 {
		if _, err := svc.LogDose(ctx, user, inhaler.ID, "", nil); err != nil {
			t.Fatal(err)
		}
	}
	day, err := svc.Today(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if len(day.AsNeeded) != 1 || len(day.AsNeeded[0].Doses) != 2 {
		t.Fatalf("as needed = %+v, want two doses", day.AsNeeded)
	}
}

func TestUndoPutsTheSlotBack(t *testing.T) {
	svc, user := newService(t, morning)
	ctx := context.Background()
	med := addMetformin(t, svc, user)

	dose, err := svc.LogDose(ctx, user, med.ID, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.UndoDose(ctx, user, dose.ID); err != nil {
		t.Fatal(err)
	}
	day, err := svc.Today(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if day.Slots[0].Log != nil {
		t.Errorf("08:00 still answered after undo: %+v", day.Slots[0].Log)
	}
	if err := svc.UndoDose(ctx, user, dose.ID); !apperr.Is(err, apperr.ErrNotFound) {
		t.Errorf("second undo: err = %v, want not found", err)
	}
}

func TestFindByNameResolvesOrExplains(t *testing.T) {
	svc, user := newService(t, morning)
	ctx := context.Background()
	addMetformin(t, svc, user)
	if _, err := svc.Add(ctx, user, medications.Input{Name: "Metoprolol", Dose: "25 mg", Times: []string{"08:00"}}); err != nil {
		t.Fatal(err)
	}

	if med, err := svc.FindByName(ctx, user, "METFORMIN"); err != nil || med.Name != "Metformin" {
		t.Errorf("exact = %+v, %v", med, err)
	}
	_, err := svc.FindByName(ctx, user, "met")
	if !apperr.Is(err, apperr.ErrValidation) || !strings.Contains(err.Error(), "Metoprolol") {
		t.Errorf("ambiguous: err = %v, want both names listed", err)
	}
	if _, err := svc.FindByName(ctx, user, "aspirin"); !apperr.Is(err, apperr.ErrNotFound) {
		t.Errorf("none: err = %v, want not found", err)
	}
}

func TestStopKeepsHistoryAndFreesTheName(t *testing.T) {
	svc, user := newService(t, morning)
	ctx := context.Background()
	med := addMetformin(t, svc, user)
	if _, err := svc.LogDose(ctx, user, med.ID, "", nil); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.Stop(ctx, user, med.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.LogDose(ctx, user, med.ID, "", nil); !apperr.Is(err, apperr.ErrNotFound) {
		t.Errorf("logging a stopped medication: err = %v", err)
	}
	active, err := svc.List(ctx, user, true)
	if err != nil || len(active) != 0 {
		t.Fatalf("active = %+v, %v", active, err)
	}
	meds, doses, err := svc.History(ctx, user)
	if err != nil || len(meds) != 1 || len(doses) != 1 {
		t.Fatalf("history = %d meds, %d doses, %v; want the stopped one and its dose", len(meds), len(doses), err)
	}
	addMetformin(t, svc, user) // the name is free again
}

func TestUpdatePatchesOnlyWhatIsGiven(t *testing.T) {
	svc, user := newService(t, morning)
	ctx := context.Background()
	med := addMetformin(t, svc, user)

	dose, off := "850 mg", false
	got, err := svc.Update(ctx, user, med.ID, medications.Patch{Dose: &dose, Remind: &off})
	if err != nil {
		t.Fatal(err)
	}
	if got.Dose != "850 mg" || got.Remind || got.Name != "Metformin" || len(got.Times) != 2 {
		t.Errorf("updated = %+v", got)
	}
}

func TestDueRemindersUseTheLocalClock(t *testing.T) {
	svc, user := newService(t, morning)
	ctx := context.Background()
	addMetformin(t, svc, user)

	// 07:30 UTC is 08:30 in Lisbon: the morning dose is due.
	due, err := svc.DueReminders(ctx, user, time.Date(2026, 10, 8, 7, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 1 || due[0].Slot != "08:00" {
		t.Fatalf("due = %+v, want 08:00", due)
	}
	// 06:30 UTC is 07:30 in Lisbon: nothing yet.
	if due, _ = svc.DueReminders(ctx, user, time.Date(2026, 10, 8, 6, 30, 0, 0, time.UTC)); len(due) != 0 {
		t.Errorf("before 08:00 local: %+v", due)
	}
}

func TestContextSourceSummarisesTodayAndStaysQuietWithoutMedications(t *testing.T) {
	svc, user := newService(t, morning)
	ctx := context.Background()

	var empty coach.Context
	if err := medications.NewContextSource(svc).Collect(ctx, coach.ContextRequest{User: user}, &empty); err != nil {
		t.Fatal(err)
	}
	if len(empty.DailySignals) != 0 {
		t.Errorf("no medications, yet DailySignals = %v", empty.DailySignals)
	}

	med := addMetformin(t, svc, user)
	if _, err := svc.LogDose(ctx, user, med.ID, "", nil); err != nil {
		t.Fatal(err)
	}
	var into coach.Context
	if err := medications.NewContextSource(svc).Collect(ctx, coach.ContextRequest{User: user}, &into); err != nil {
		t.Fatal(err)
	}
	want := "Metformin 500 mg — 08:00 taken, 20:00 upcoming"
	if len(into.DailySignals) != 1 || !strings.Contains(into.DailySignals[0], want) {
		t.Errorf("DailySignals = %v, want %q", into.DailySignals, want)
	}
}
