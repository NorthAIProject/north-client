package medication

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func ptr(s string) *string { return &s }

// Thursday 8 October 2026.
func at(hour, minute int) time.Time {
	return time.Date(2026, 10, 8, hour, minute, 0, 0, time.UTC)
}

func metformin() Medication {
	return Medication{
		ID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), Name: "Metformin", Dose: "500 mg",
		Times: []string{"08:00", "20:00"}, Days: AllDays(), Remind: true,
	}
}

func inhaler() Medication {
	return Medication{
		ID: uuid.MustParse("22222222-2222-2222-2222-222222222222"), Name: "Salbutamol", Dose: "1 puff",
		Days: AllDays(), Remind: true,
	}
}

func TestValidateNormalisesTheSchedule(t *testing.T) {
	t.Parallel()

	got, err := Validate(Input{Name: "  Metformin ", Dose: " 500 mg ", Times: []string{"20:00", "8:00", "08:00"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Metformin" || got.Dose != "500 mg" {
		t.Errorf("not trimmed: %+v", got)
	}
	if strings.Join(got.Times, ",") != "08:00,20:00" {
		t.Errorf("times = %v, want sorted, padded and deduplicated", got.Times)
	}
	if len(got.Days) != 7 {
		t.Errorf("days = %v, want every day by default", got.Days)
	}
}

func TestValidateRejectsWhatCannotBeScheduled(t *testing.T) {
	t.Parallel()

	for name, in := range map[string]Input{
		"no name":   {Dose: "5 mg"},
		"long dose": {Name: "X", Dose: strings.Repeat("m", 41)},
		"bad time":  {Name: "X", Times: []string{"25:00"}},
		"bad day":   {Name: "X", Days: []int{7}},
	} {
		if _, err := Validate(in); !apperr.Is(err, apperr.ErrValidation) {
			t.Errorf("%s: err = %v, want a validation error", name, err)
		}
	}
}

func TestSlotsForRespectsDaysAndAsNeeded(t *testing.T) {
	t.Parallel()

	weekdays := metformin()
	weekdays.Days = []int{1, 2, 3, 4, 5}
	if got := SlotsFor(weekdays, time.Saturday); got != nil {
		t.Errorf("Saturday slots = %v, want none", got)
	}
	if got := SlotsFor(weekdays, time.Thursday); len(got) != 2 {
		t.Errorf("Thursday slots = %v", got)
	}
	if got := SlotsFor(inhaler(), time.Thursday); got != nil {
		t.Errorf("as-needed slots = %v, want none", got)
	}
}

func TestPickSlotTakesTheNearestUnloggedTime(t *testing.T) {
	t.Parallel()

	m := metformin()
	if got, ok := PickSlot(m, nil, at(9, 30)); !ok || *got != "08:00" {
		t.Errorf("09:30 with nothing logged = %v, want 08:00", got)
	}
	if got, ok := PickSlot(m, nil, at(18, 0)); !ok || *got != "20:00" {
		t.Errorf("18:00 = %v, want 20:00", got)
	}
	// The morning dose is answered, so a late-morning log is the evening one.
	logs := []Dose{{MedicationID: m.ID, Slot: ptr("08:00"), Status: StatusTaken}}
	if got, ok := PickSlot(m, logs, at(10, 0)); !ok || *got != "20:00" {
		t.Errorf("with 08:00 logged = %v, want 20:00", got)
	}
	logs = append(logs, Dose{MedicationID: m.ID, Slot: ptr("20:00"), Status: StatusSkipped})
	if _, ok := PickSlot(m, logs, at(21, 0)); ok {
		t.Error("every slot answered, yet a slot was picked")
	}
}

func TestPickSlotLeavesAsNeededUnslotted(t *testing.T) {
	t.Parallel()

	if got, ok := PickSlot(inhaler(), nil, at(14, 0)); !ok || got != nil {
		t.Errorf("as needed = %v, %v; want nil, true", got, ok)
	}
}

func TestBuildDayAndSummary(t *testing.T) {
	t.Parallel()

	m, inh := metformin(), inhaler()
	logs := []Dose{
		{MedicationID: m.ID, Slot: ptr("08:00"), Status: StatusTaken, LoggedAt: at(8, 5)},
		{MedicationID: inh.ID, Status: StatusTaken, LoggedAt: at(14, 10)},
	}
	day := BuildDay([]Medication{m, inh}, logs, at(15, 0))
	if len(day.Slots) != 2 || len(day.AsNeeded) != 1 {
		t.Fatalf("day = %+v", day)
	}

	got := Summary(day)
	want := "Medications today: Metformin 500 mg — 08:00 taken, 20:00 upcoming; Salbutamol 1 puff (as needed) — taken 1× (14:10)."
	if got != want {
		t.Errorf("Summary =\n %q\nwant\n %q", got, want)
	}
	if got := Summary(BuildDay([]Medication{m, inh}, logs, at(20, 30))); !strings.Contains(got, "20:00 due") {
		t.Errorf("evening summary = %q, want 20:00 due", got)
	}
	if got := Summary(Day{Now: at(9, 0)}); got != "Medications: none tracked." {
		t.Errorf("empty = %q", got)
	}
}

func TestDueRemindersOnlyInsideTheWindow(t *testing.T) {
	t.Parallel()

	m := metformin()
	quiet := inhaler()
	due := func(meds []Medication, logs []Dose, now time.Time) []Reminder {
		return DueReminders(BuildDay(meds, logs, now), ReminderWindow)
	}

	if got := due([]Medication{m, quiet}, nil, at(7, 59)); len(got) != 0 {
		t.Errorf("before 08:00: %+v", got)
	}
	got := due([]Medication{m, quiet}, nil, at(8, 30))
	if len(got) != 1 || got[0].Slot != "08:00" {
		t.Fatalf("08:30: %+v", got)
	}
	if got := due([]Medication{m, quiet}, nil, at(10, 1)); len(got) != 0 {
		t.Errorf("past the window: %+v", got)
	}

	if got := due([]Medication{m}, []Dose{{MedicationID: m.ID, Slot: ptr("08:00"), Status: StatusSkipped}}, at(8, 30)); len(got) != 0 {
		t.Errorf("a skipped slot was reminded: %+v", got)
	}

	m.Remind = false
	if got := due([]Medication{m}, nil, at(8, 30)); len(got) != 0 {
		t.Errorf("remind off, still reminded: %+v", got)
	}
}

func TestMatchingPrefersAnExactName(t *testing.T) {
	t.Parallel()

	xr := metformin()
	xr.ID, xr.Name = uuid.New(), "Metformin XR"
	meds := []Medication{metformin(), xr, inhaler()}

	if got := Matching(meds, "metformin"); len(got) != 1 || got[0].Name != "Metformin" {
		t.Errorf("exact = %v", Names(got))
	}
	if got := Matching(meds, "met"); len(got) != 2 {
		t.Errorf("substring = %v, want both metformins", Names(got))
	}
	if got := Matching(meds, "aspirin"); len(got) != 0 {
		t.Errorf("none = %v", Names(got))
	}
}
