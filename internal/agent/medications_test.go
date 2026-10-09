package agent

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/medications"
	"github.com/NorthAIProject/north-client/internal/medications/medication"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

func TestMedicationDaysReadsNamesAsWeekdayIndices(t *testing.T) {
	t.Parallel()

	got, err := medicationDays([]string{"Monday", "wed", "FRI", "monday"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []int{1, 3, 5}) {
		t.Errorf("days = %v, want [1 3 5]", got)
	}
	if got, err := medicationDays(nil); err != nil || len(got) != 0 {
		t.Errorf("no days = %v, %v; want empty for every day", got, err)
	}
	if _, err := medicationDays([]string{"Funday"}); !apperr.Is(err, apperr.ErrValidation) {
		t.Errorf("a made-up day: err = %v, want a validation error the model can read", err)
	}
}

func TestMedicationSlotIsOptionalButMustBeAClockTime(t *testing.T) {
	t.Parallel()

	if got, err := medicationSlot(""); err != nil || got != nil {
		t.Errorf("empty = %v, %v; want nil", got, err)
	}
	if got, err := medicationSlot("8:00"); err != nil || *got != "08:00" {
		t.Errorf("8:00 = %v, %v; want 08:00", got, err)
	}
	if _, err := medicationSlot("eight"); !apperr.Is(err, apperr.ErrValidation) {
		t.Errorf("eight: err = %v", err)
	}
}

// What the model reads back carries names, doses and times — never an id.
func TestDescribeMedicationsNeverShowsAnID(t *testing.T) {
	t.Parallel()

	m := medications.Medication{
		ID: uuid.New(), Name: "Metformin", Dose: "500 mg", Times: []string{"08:00", "20:00"},
		Days: medication.AllDays(), Remind: true,
	}
	day := medication.BuildDay([]medications.Medication{m}, nil, time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC))
	got := describeMedications([]medications.Medication{m}, day)

	for _, want := range []string{"Metformin 500 mg", "08:00, 20:00 · every day", "reminders on", "08:00 due"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, m.ID.String()) {
		t.Errorf("the id leaked to the model:\n%s", got)
	}
}
