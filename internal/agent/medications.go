package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/medications"
	"github.com/NorthAIProject/north-client/internal/medications/medication"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// Medications by conversation: "I take 500 mg of metformin at 8 and 8",
// "took my evening metformin", "I've stopped the ibuprofen".
//
// North is a record, not a prescriber. Every description below says so,
// because a model asked "should I take another?" will otherwise reach for the
// tool that writes doses. The tools store what the person states and nothing
// else; none of them suggests, changes or comments on a dose.
//
// Medications are named, never numbered: the model passes the name as the
// person said it, resolved by medications.Service.FindByName, which lists the
// candidates when it is ambiguous. No id reaches the model in either
// direction. Only list_medications is ReadOnly, so every write shows the
// person an approval card first.

const medicationRule = " Record only what the person states; never suggest, change, or advise on doses."

func listMedications(svc *medications.Service, userSvc *users.Service) Capability {
	return Capability{
		Tool: ai.Tool{
			Name: "list_medications",
			Description: "Read the medications this person tracks: each one's dose and schedule as they gave it, and what was " +
				"taken, skipped or is still due today." + medicationRule,
			Parameters: ai.Object("no arguments", map[string]*ai.Schema{}),
		},
		ReadOnly:   true,
		Idempotent: true,
		Invoke: func(ctx context.Context, userID uuid.UUID, _ json.RawMessage) (string, error) {
			user, err := userSvc.ByID(ctx, userID)
			if err != nil {
				return "", err
			}
			meds, err := svc.List(ctx, user, true)
			if err != nil {
				return "", err
			}
			if len(meds) == 0 {
				return "No medications are tracked.", nil
			}
			day, err := svc.Today(ctx, user)
			if err != nil {
				return "", err
			}
			return describeMedications(meds, day), nil
		},
	}
}

func addMedication(svc *medications.Service, userSvc *users.Service) Capability {
	type args struct {
		Name   string   `json:"name"`
		Dose   string   `json:"dose"`
		Times  []string `json:"times"`
		Days   []string `json:"days"`
		Remind *bool    `json:"remind"`
		Notes  string   `json:"notes"`
	}
	return Capability{
		Tool: ai.Tool{
			Name: "add_medication",
			Description: "Start tracking a medication the person says they take, with the dose and times exactly as they said them. " +
				"No times means as needed. Reminders are on unless they say otherwise." + medicationRule,
			Parameters: ai.Object("the medication", map[string]*ai.Schema{
				"name":   ai.String("its name as they said it"),
				"dose":   ai.String("the dose as they said it, like '500 mg' or '1 puff'; empty when they did not say"),
				"times":  ai.Array("times of day as HH:MM in their time; empty for as needed", ai.String("HH:MM")),
				"days":   ai.Array("weekday names when not every day; empty for every day", ai.String("a weekday, like Monday")),
				"remind": ai.Boolean("whether to remind them at each time; defaults to true"),
				"notes":  ai.String("anything else they said about taking it; optional"),
			}, "name"),
		},
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}
			days, err := medicationDays(in.Days)
			if err != nil {
				return "", err
			}
			user, err := userSvc.ByID(ctx, userID)
			if err != nil {
				return "", err
			}
			med, err := svc.Add(ctx, user, medications.Input{
				Name: in.Name, Dose: in.Dose, Times: in.Times, Days: days,
				Remind: in.Remind == nil || *in.Remind, Notes: in.Notes,
			})
			if err != nil {
				return "", err
			}
			return "Now tracking " + describeMedication(med) + ".", nil
		},
	}
}

func updateMedication(svc *medications.Service, userSvc *users.Service) Capability {
	type args struct {
		Name    string    `json:"name"`
		NewName *string   `json:"new_name"`
		Dose    *string   `json:"dose"`
		Times   *[]string `json:"times"`
		Days    *[]string `json:"days"`
		Remind  *bool     `json:"remind"`
	}
	return Capability{
		Tool: ai.Tool{
			Name: "update_medication",
			Description: "Change a tracked medication because the person said it changed: a new dose, new times, other days, " +
				"reminders on or off, or a corrected name. Pass only what changed." + medicationRule,
			Parameters: ai.Object("the change", map[string]*ai.Schema{
				"name":     ai.String("the medication as it is tracked now"),
				"new_name": ai.String("its corrected name; optional"),
				"dose":     ai.String("the new dose as they said it; optional"),
				"times":    ai.Array("the new times as HH:MM; an empty list makes it as needed; optional", ai.String("HH:MM")),
				"days":     ai.Array("the new weekday names; an empty list means every day; optional", ai.String("a weekday")),
				"remind":   ai.Boolean("reminders on or off; optional"),
			}, "name"),
		},
		Idempotent: true,
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}
			patch := medications.Patch{Name: in.NewName, Dose: in.Dose, Times: in.Times, Remind: in.Remind}
			if in.Days != nil {
				days, err := medicationDays(*in.Days)
				if err != nil {
					return "", err
				}
				patch.Days = &days
			}
			user, err := userSvc.ByID(ctx, userID)
			if err != nil {
				return "", err
			}
			med, err := svc.FindByName(ctx, user, in.Name)
			if err != nil {
				return "", err
			}
			updated, err := svc.Update(ctx, user, med.ID, patch)
			if err != nil {
				return "", err
			}
			return "Updated: " + describeMedication(updated) + ".", nil
		},
	}
}

func stopMedication(svc *medications.Service, userSvc *users.Service) Capability {
	type args struct {
		Name string `json:"name"`
	}
	return Capability{
		Tool: ai.Tool{
			Name: "stop_medication",
			Description: "Stop tracking a medication the person says they no longer take. Its logged doses are kept as history, " +
				"and its reminders end." + medicationRule,
			Parameters: ai.Object("the medication", map[string]*ai.Schema{
				"name": ai.String("the medication as it is tracked"),
			}, "name"),
		},
		Idempotent: true,
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}
			user, err := userSvc.ByID(ctx, userID)
			if err != nil {
				return "", err
			}
			med, err := svc.FindByName(ctx, user, in.Name)
			if err != nil {
				return "", err
			}
			if _, err := svc.Stop(ctx, user, med.ID); err != nil {
				return "", err
			}
			return "Stopped tracking " + med.Label() + ". Its history is kept.", nil
		},
	}
}

func logMedicationDose(svc *medications.Service, userSvc *users.Service) Capability {
	type args struct {
		Name   string `json:"name"`
		Status string `json:"status"`
		Time   string `json:"time"`
	}
	return Capability{
		Tool: ai.Tool{
			Name: "log_medication_dose",
			Description: "Record that the person took or skipped a dose of a tracked medication today. Without a time, it answers " +
				"the scheduled time nearest now that is still open; logging a time again replaces its answer." + medicationRule,
			Parameters: ai.Object("the dose", map[string]*ai.Schema{
				"name":   ai.String("the medication as it is tracked"),
				"status": ai.Enum("taken unless they said they skipped it", medication.StatusTaken, medication.StatusSkipped),
				"time":   ai.String("the scheduled HH:MM this dose was for, when they said; optional"),
			}, "name"),
		},
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}
			slot, err := medicationSlot(in.Time)
			if err != nil {
				return "", err
			}
			user, err := userSvc.ByID(ctx, userID)
			if err != nil {
				return "", err
			}
			med, err := svc.FindByName(ctx, user, in.Name)
			if err != nil {
				return "", err
			}
			dose, err := svc.LogDose(ctx, user, med.ID, in.Status, slot)
			if err != nil {
				return "", err
			}
			out := fmt.Sprintf("Logged %s as %s", med.Label(), dose.Status)
			if dose.Slot != nil {
				out += " for " + *dose.Slot
			}
			return out + ".", nil
		},
	}
}

// medicationDays reads weekday names ("Monday", "mon") as 0=Sunday .. 6
// indices. Empty means every day, which the service fills in.
func medicationDays(names []string) ([]int, error) {
	out := make([]int, 0, len(names))
	for _, name := range names {
		needle := strings.ToLower(strings.TrimSpace(name))
		found := -1
		for d := time.Sunday; d <= time.Saturday; d++ {
			if len(needle) >= 2 && strings.HasPrefix(strings.ToLower(d.String()), needle) {
				found = int(d)
				break
			}
		}
		if found < 0 {
			return nil, apperr.Wrap(apperr.ErrValidation, "%q is not a day of the week", name)
		}
		if !slices.Contains(out, found) {
			out = append(out, found)
		}
	}
	return out, nil
}

// medicationSlot reads an optional "HH:MM", nil when empty.
func medicationSlot(raw string) (*string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	t, ok := medication.NormalizeTime(raw)
	if !ok {
		return nil, apperr.Wrap(apperr.ErrValidation, "time %q must be HH:MM", raw)
	}
	return &t, nil
}

// describeMedication is "Metformin 500 mg, 08:00, 20:00 · every day, reminders
// on": what was stored, read back so the person can catch a mistake.
func describeMedication(m medications.Medication) string {
	out := m.Label() + ", " + medication.Schedule(m)
	if !m.AsNeeded() {
		if m.Remind {
			out += ", reminders on"
		} else {
			out += ", reminders off"
		}
	}
	return out
}

func describeMedications(meds []medications.Medication, day medications.Day) string {
	var b strings.Builder
	for _, m := range meds {
		b.WriteString(describeMedication(m))
		if m.Notes != "" {
			b.WriteString(" (" + m.Notes + ")")
		}
		b.WriteString(".\n")
	}
	b.WriteString(medication.Summary(day))
	return b.String()
}
