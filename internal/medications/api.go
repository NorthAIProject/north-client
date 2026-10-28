package medications

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/medications/medication"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is medications for native clients. Every change answers with the
// active medications and today, so a client never works out which slot is due.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Register(g *echo.Group) {
	g.GET("/medications", a.show)
	g.POST("/medications", a.add)
	g.PUT("/medications/:medicationID", a.update)
	g.POST("/medications/:medicationID/stop", a.stop)
	g.POST("/medications/:medicationID/doses", a.logDose)
	g.DELETE("/medications/doses/:logID", a.undoDose)
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

func (a *API) show(c *echo.Context) error { return a.respond(c, http.StatusOK) }

func (a *API) add(c *echo.Context) error {
	var req MedicationRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 8 << 10}, "The request body could not be read."); err != nil {
		return err
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
	ctx := c.Request().Context()
	if _, err := a.svc.Add(ctx, auth.MustUser(ctx), in); err != nil {
		return httpx.Fail(err, "The medication could not be added.")
	}
	return a.respond(c, http.StatusCreated)
}

func (a *API) update(c *echo.Context) error {
	id, err := pathID(c, "medicationID")
	if err != nil {
		return err
	}
	var req MedicationRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 8 << 10}, "The request body could not be read."); err != nil {
		return err
	}
	patch := Patch{Name: req.Name, Dose: req.Dose, Times: req.Times, Days: req.DaysOfWeek, Remind: req.Remind, Notes: req.Notes}
	ctx := c.Request().Context()
	if _, err := a.svc.Update(ctx, auth.MustUser(ctx), id, patch); err != nil {
		return httpx.Fail(err, "The medication could not be changed.")
	}
	return a.respond(c, http.StatusOK)
}

func (a *API) stop(c *echo.Context) error {
	id, err := pathID(c, "medicationID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if _, err := a.svc.Stop(ctx, auth.MustUser(ctx), id); err != nil {
		return httpx.Fail(err, "The medication could not be stopped.")
	}
	return a.respond(c, http.StatusOK)
}

func (a *API) logDose(c *echo.Context) error {
	id, err := pathID(c, "medicationID")
	if err != nil {
		return err
	}
	var req DoseRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 4 << 10}, "The request body could not be read."); err != nil {
		return err
	}
	ctx := c.Request().Context()
	if _, err := a.svc.LogDose(ctx, auth.MustUser(ctx), id, req.Status, req.Slot); err != nil {
		return httpx.Fail(err, "The dose could not be logged.")
	}
	return a.respond(c, http.StatusCreated)
}

func (a *API) undoDose(c *echo.Context) error {
	id, err := pathID(c, "logID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err := a.svc.UndoDose(ctx, auth.MustUser(ctx), id); err != nil {
		return httpx.Fail(err, "That could not be undone.")
	}
	return a.respond(c, http.StatusOK)
}

func (a *API) respond(c *echo.Context, status int) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	meds, err := a.svc.List(ctx, user, true)
	if err != nil {
		return httpx.Fail(err, "Your medications could not be loaded.")
	}
	day, err := a.svc.Today(ctx, user)
	if err != nil {
		return httpx.Fail(err, "Today's medications could not be loaded.")
	}
	return c.JSON(status, Project(meds, day))
}

// pathID reads a medication or dose id; one that does not parse names
// nothing, so it is a 404 in this API's own words.
func pathID(c *echo.Context, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		return uuid.Nil, httpx.Fail(apperr.ErrNotFound, "No such medication or dose.")
	}
	return id, nil
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
