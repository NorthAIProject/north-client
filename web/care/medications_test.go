package care

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/medications/medication"
	"github.com/NorthAIProject/north-client/internal/users"
)

// A slot not yet answered offers Taken and Skip; an answered one offers Undo;
// an as-needed medication offers "Took one". Each posts where the medications
// handler listens.
func TestMedicationsCardOffersTheRightAction(t *testing.T) {
	metformin := medication.Medication{
		ID: uuid.New(), Name: "Metformin", Dose: "500 mg", Times: []string{"08:00", "20:00"},
		Days: medication.AllDays(), Remind: true,
	}
	inhaler := medication.Medication{ID: uuid.New(), Name: "Salbutamol", Dose: "1 puff", Days: medication.AllDays()}
	morning := "08:00"
	logID := uuid.New()
	meds := []medication.Medication{metformin, inhaler}
	day := medication.BuildDay(meds, []medication.Dose{
		{ID: logID, MedicationID: metformin.ID, Slot: &morning, Status: medication.StatusTaken},
	}, time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC))

	var b strings.Builder
	data := Data{Medications: meds, MedicationDay: day}
	if err := Panel(users.User{DisplayName: "Ana"}, data, Forms{}).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	html := b.String()

	for _, want := range []string{
		"/app/care/medications/doses/" + logID.String() + "/undo",
		"/app/care/medications/" + metformin.ID.String() + "/doses",
		`name="slot" value="20:00"`,
		"Skip",
		"Took one",
		"/app/care/medications/" + inhaler.ID.String() + "/stop",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the card is missing %q", want)
		}
	}
	if strings.Contains(html, `name="slot" value="08:00"`) {
		t.Error("the answered 08:00 slot still offers a dose button")
	}
}
