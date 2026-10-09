package medications

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/medications/medication"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is medications for native clients. Every change answers with the
// active medications and today, so a client never works out which slot is due.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/medications", a.show)
	r.Post("/medications", a.add)
	r.Put("/medications/{medicationID}", a.update)
	r.Post("/medications/{medicationID}/stop", a.stop)
	r.Post("/medications/{medicationID}/doses", a.logDose)
	r.Delete("/medications/doses/{logID}", a.undoDose)
}

// MedicationRequest adds a medication (name required; remind defaults to
// true) or, on PUT, changes only the fields present.
type MedicationRequest struct {
	Name *string `json:"name,omitempty"`
	Dose *string `json:"dose,omitempty"`
	// Times are HH:MM; empty means as needed.
	Times *[]string `json:"times,omitempty"`
	// DaysOfWeek are 0 (Sunday) to 6; omitted or empty means every day.
	DaysOfWeek *[]int  `json:"daysOfWeek,omitempty"`
	Remind     *bool   `json:"remind,omitempty"`
	Notes      *string `json:"notes,omitempty"`
}

// DoseRequest logs a dose. Status is "taken" (the default) or "skipped"; with
// no slot, the nearest unanswered time today is used.
type DoseRequest struct {
	Status string  `json:"status,omitempty"`
	Slot   *string `json:"slot,omitempty"`
}

type MedicationView struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	Dose       string    `json:"dose"`
	Times      []string  `json:"times"`
	DaysOfWeek []int     `json:"daysOfWeek"`
	Remind     bool      `json:"remind"`
	Notes      string    `json:"notes"`
	AsNeeded   bool      `json:"asNeeded"`
}

// MedicationSlotView is one scheduled dose today. Status is a plain string —
// taken, skipped, due or upcoming today — and a client must accept others.
type MedicationSlotView struct {
	MedicationID uuid.UUID `json:"medicationId"`
	Name         string    `json:"name"`
	Dose         string    `json:"dose"`
	Time         string    `json:"time"`
	Status       string    `json:"status"`
	// LogID is the dose that answered the slot, for undo.
	LogID *uuid.UUID `json:"logId,omitempty"`
}

type MedicationDoseView struct {
	ID           uuid.UUID `json:"id"`
	MedicationID uuid.UUID `json:"medicationId"`
	Name         string    `json:"name"`
	Slot         *string   `json:"slot,omitempty"`
	Status       string    `json:"status"`
	LoggedAt     time.Time `json:"loggedAt"`
}

type MedicationAsNeededView struct {
	MedicationID uuid.UUID            `json:"medicationId"`
	Name         string               `json:"name"`
	Dose         string               `json:"dose"`
	Doses        []MedicationDoseView `json:"doses"`
}

type MedicationsTodayView struct {
	// Date is the person's local date, YYYY-MM-DD.
	Date     string                   `json:"date"`
	Slots    []MedicationSlotView     `json:"slots"`
	AsNeeded []MedicationAsNeededView `json:"asNeeded"`
}

type MedicationsView struct {
	Medications []MedicationView     `json:"medications"`
	Today       MedicationsTodayView `json:"today"`
}

func (a *API) show(w http.ResponseWriter, r *http.Request) { a.respond(w, r, http.StatusOK) }

func (a *API) add(w http.ResponseWriter, r *http.Request) {
	var req MedicationRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 8 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return
	}
	in := Input{Remind: true}
	if req.Name != nil {
		in.Name = *req.Name
	}
	if req.Dose != nil {
		in.Dose = *req.Dose
	}
	if req.Times != nil {
		in.Times = *req.Times
	}
	if req.DaysOfWeek != nil {
		in.Days = *req.DaysOfWeek
	}
	if req.Remind != nil {
		in.Remind = *req.Remind
	}
	if req.Notes != nil {
		in.Notes = *req.Notes
	}
	if _, err := a.svc.Add(r.Context(), auth.MustUser(r.Context()), in); err != nil {
		httpx.Error(w, err, "The medication could not be added.")
		return
	}
	a.respond(w, r, http.StatusCreated)
}

func (a *API) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "medicationID")
	if !ok {
		return
	}
	var req MedicationRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 8 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return
	}
	patch := Patch{Name: req.Name, Dose: req.Dose, Times: req.Times, Days: req.DaysOfWeek, Remind: req.Remind, Notes: req.Notes}
	if _, err := a.svc.Update(r.Context(), auth.MustUser(r.Context()), id, patch); err != nil {
		httpx.Error(w, err, "The medication could not be changed.")
		return
	}
	a.respond(w, r, http.StatusOK)
}

func (a *API) stop(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "medicationID")
	if !ok {
		return
	}
	if _, err := a.svc.Stop(r.Context(), auth.MustUser(r.Context()), id); err != nil {
		httpx.Error(w, err, "The medication could not be stopped.")
		return
	}
	a.respond(w, r, http.StatusOK)
}

func (a *API) logDose(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "medicationID")
	if !ok {
		return
	}
	var req DoseRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 4 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return
	}
	if _, err := a.svc.LogDose(r.Context(), auth.MustUser(r.Context()), id, req.Status, req.Slot); err != nil {
		httpx.Error(w, err, "The dose could not be logged.")
		return
	}
	a.respond(w, r, http.StatusCreated)
}

func (a *API) undoDose(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "logID")
	if !ok {
		return
	}
	if err := a.svc.UndoDose(r.Context(), auth.MustUser(r.Context()), id); err != nil {
		httpx.Error(w, err, "That could not be undone.")
		return
	}
	a.respond(w, r, http.StatusOK)
}

func (a *API) respond(w http.ResponseWriter, r *http.Request, status int) {
	user := auth.MustUser(r.Context())
	meds, err := a.svc.List(r.Context(), user, true)
	if err != nil {
		httpx.Error(w, err, "Your medications could not be loaded.")
		return
	}
	day, err := a.svc.Today(r.Context(), user)
	if err != nil {
		httpx.Error(w, err, "Today's medications could not be loaded.")
		return
	}
	httpx.WriteJSON(w, status, Project(meds, day))
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "No such medication or dose.")
		return uuid.Nil, false
	}
	return id, true
}

// Project is the medications payload.
func Project(meds []Medication, day Day) MedicationsView {
	out := MedicationsView{
		Medications: make([]MedicationView, len(meds)),
		Today: MedicationsTodayView{
			Date:     day.Now.Format("2006-01-02"),
			Slots:    make([]MedicationSlotView, len(day.Slots)),
			AsNeeded: make([]MedicationAsNeededView, len(day.AsNeeded)),
		},
	}
	for i, m := range meds {
		out.Medications[i] = MedicationView{
			ID: m.ID, Name: m.Name, Dose: m.Dose, Times: append([]string{}, m.Times...),
			DaysOfWeek: append([]int{}, m.Days...), Remind: m.Remind, Notes: m.Notes, AsNeeded: m.AsNeeded(),
		}
	}
	for i, s := range day.Slots {
		v := MedicationSlotView{
			MedicationID: s.Medication.ID, Name: s.Medication.Name, Dose: s.Medication.Dose,
			Time: s.Time, Status: s.Status(day.Now),
		}
		if s.Log != nil {
			v.LogID = &s.Log.ID
		}
		out.Today.Slots[i] = v
	}
	for i, u := range day.AsNeeded {
		v := MedicationAsNeededView{
			MedicationID: u.Medication.ID, Name: u.Medication.Name, Dose: u.Medication.Dose,
			Doses: make([]MedicationDoseView, len(u.Doses)),
		}
		for j, d := range u.Doses {
			v.Doses[j] = doseView(d, u.Medication)
		}
		out.Today.AsNeeded[i] = v
	}
	return out
}

func doseView(d Dose, m medication.Medication) MedicationDoseView {
	return MedicationDoseView{ID: d.ID, MedicationID: m.ID, Name: m.Name, Slot: d.Slot, Status: d.Status, LoggedAt: d.LoggedAt}
}
