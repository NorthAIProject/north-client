package medications_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/medications"
	"github.com/NorthAIProject/north-client/internal/medications/medication"
	"github.com/NorthAIProject/north-client/internal/shared/apitest"
)

func TestMedicationsShape(t *testing.T) {
	t.Parallel()

	metformin := medications.Medication{
		ID: uuid.MustParse("44444444-4444-4444-4444-444444444444"), Name: "Metformin", Dose: "500 mg",
		Times: []string{"08:00", "20:00"}, Days: medication.AllDays(), Remind: true,
	}
	inhaler := medications.Medication{
		ID: uuid.MustParse("55555555-5555-5555-5555-555555555555"), Name: "Salbutamol", Dose: "1 puff",
		Days: medication.AllDays(), Remind: true, Notes: "Before a run if it is cold.",
	}
	morning := "08:00"
	logs := []medications.Dose{
		{
			ID: uuid.MustParse("66666666-6666-6666-6666-666666666666"), MedicationID: metformin.ID,
			Slot: &morning, Status: medication.StatusTaken, LoggedAt: time.Date(2026, 10, 8, 8, 5, 0, 0, time.UTC),
		},
		{
			ID: uuid.MustParse("77777777-7777-7777-7777-777777777777"), MedicationID: inhaler.ID,
			Status: medication.StatusTaken, LoggedAt: time.Date(2026, 10, 8, 14, 10, 0, 0, time.UTC),
		},
	}
	meds := []medications.Medication{metformin, inhaler}
	day := medication.BuildDay(meds, logs, time.Date(2026, 10, 8, 15, 0, 0, 0, time.UTC))

	apitest.AssertGolden(t, "medications.golden.json", medications.Project(meds, day))
}
