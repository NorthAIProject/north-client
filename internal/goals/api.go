package goals

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/lifedomain"
)

// dateLayout is how a goal's or milestone's target date travels: a calendar
// day, with no time or zone, exactly as the web form sends it.
const dateLayout = "2006-01-02"

// API is goals for native clients: the web /goals pages' list, detail,
// create, edit, status, progress notes and milestones, over the same service.
type API struct {
	svc *Service
}

// NewAPI builds the routes; mount them behind auth.RequireBearer.
func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/goals", a.list)
	r.Post("/goals", a.create)
	r.Get("/goals/{goalID}", a.show)
	r.Put("/goals/{goalID}", a.update)
	r.Put("/goals/{goalID}/status", a.setStatus)
	r.Delete("/goals/{goalID}", a.destroy)
	r.Post("/goals/{goalID}/updates", a.addUpdate)
	r.Post("/goals/{goalID}/milestones", a.addMilestone)
	r.Put("/goals/{goalID}/milestones/{milestoneID}", a.updateMilestone)
	r.Put("/goals/{goalID}/milestones/{milestoneID}/status", a.setMilestoneStatus)
	r.Delete("/goals/{goalID}/milestones/{milestoneID}", a.deleteMilestone)
}

type GoalUpdateView struct {
	ID   uuid.UUID `json:"id"`
	Note string    `json:"note"`
	// Progress is 0-100 when the note carried one.
	Progress  *int      `json:"progress,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

type MilestoneView struct {
	ID    uuid.UUID `json:"id"`
	Title string    `json:"title"`
	// Status is open or completed.
	Status      string     `json:"status"`
	TargetDate  string     `json:"targetDate,omitempty"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

type GoalSummary struct {
	ID       uuid.UUID `json:"id"`
	Title    string    `json:"title"`
	Category string    `json:"category"`
	// Status is active, achieved, paused or abandoned.
	Status         string          `json:"status"`
	TargetDate     string          `json:"targetDate,omitempty"`
	MilestoneTotal int             `json:"milestoneTotal"`
	MilestoneDone  int             `json:"milestoneDone"`
	LatestUpdate   *GoalUpdateView `json:"latestUpdate,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
}

type GoalList struct {
	Goals []GoalSummary `json:"goals"`
	// Categories are the life areas a goal can belong to, in display order.
	Categories []string `json:"categories"`
}

type GoalDetail struct {
	GoalSummary
	Motivation string           `json:"motivation"`
	Success    string           `json:"success"`
	Milestones []MilestoneView  `json:"milestones"`
	Updates    []GoalUpdateView `json:"updates"`
}

type GoalRequest struct {
	Title      string `json:"title"`
	Motivation string `json:"motivation"`
	Success    string `json:"success"`
	Category   string `json:"category"`
	TargetDate string `json:"targetDate,omitempty"`
}

type StatusRequest struct {
	Status string `json:"status"`
}

type UpdateRequest struct {
	Note     string `json:"note"`
	Progress *int   `json:"progress,omitempty"`
}

type MilestoneRequest struct {
	Title      string `json:"title"`
	TargetDate string `json:"targetDate,omitempty"`
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	goals, err := a.svc.List(r.Context(), auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "Goals could not be loaded.")
		return
	}
	out := GoalList{Goals: make([]GoalSummary, 0, len(goals)), Categories: lifedomain.Domains}
	for _, g := range goals {
		out.Goals = append(out.Goals, summarize(g))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	in, ok := readGoal(w, r)
	if !ok {
		return
	}
	userID := auth.MustUser(r.Context()).ID
	goal, err := a.svc.Create(r.Context(), userID, in)
	if err != nil {
		httpx.Error(w, err, "The goal could not be saved.")
		return
	}
	a.writeDetail(w, r, http.StatusCreated, goal.ID)
}

func (a *API) show(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "goalID")
	if !ok {
		return
	}
	a.writeDetail(w, r, http.StatusOK, id)
}

func (a *API) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "goalID")
	if !ok {
		return
	}
	in, ok := readGoal(w, r)
	if !ok {
		return
	}
	if _, err := a.svc.Update(r.Context(), id, auth.MustUser(r.Context()).ID, in); err != nil {
		httpx.Error(w, err, "The goal could not be saved.")
		return
	}
	a.writeDetail(w, r, http.StatusOK, id)
}

func (a *API) setStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "goalID")
	if !ok {
		return
	}
	var req StatusRequest
	if !readJSON(w, r, &req) {
		return
	}
	if _, err := a.svc.SetStatus(r.Context(), id, auth.MustUser(r.Context()).ID, req.Status); err != nil {
		httpx.Error(w, err, "The goal's status could not be changed.")
		return
	}
	a.writeDetail(w, r, http.StatusOK, id)
}

func (a *API) destroy(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "goalID")
	if !ok {
		return
	}
	if err := a.svc.Delete(r.Context(), id, auth.MustUser(r.Context()).ID); err != nil {
		httpx.Error(w, err, "The goal could not be deleted.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) addUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "goalID")
	if !ok {
		return
	}
	var req UpdateRequest
	if !readJSON(w, r, &req) {
		return
	}
	update, err := a.svc.AddUpdate(r.Context(), id, auth.MustUser(r.Context()).ID, req.Note, req.Progress)
	if err != nil {
		httpx.Error(w, err, "The note could not be saved.")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, projectUpdate(update))
}

func (a *API) addMilestone(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "goalID")
	if !ok {
		return
	}
	in, ok := readMilestone(w, r)
	if !ok {
		return
	}
	m, err := a.svc.AddMilestone(r.Context(), id, auth.MustUser(r.Context()).ID, in)
	if err != nil {
		httpx.Error(w, err, "The milestone could not be saved.")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, projectMilestone(m))
}

func (a *API) updateMilestone(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "milestoneID")
	if !ok {
		return
	}
	in, ok := readMilestone(w, r)
	if !ok {
		return
	}
	m, err := a.svc.UpdateMilestone(r.Context(), id, auth.MustUser(r.Context()).ID, in)
	if err != nil {
		httpx.Error(w, err, "The milestone could not be saved.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectMilestone(m))
}

func (a *API) setMilestoneStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "milestoneID")
	if !ok {
		return
	}
	var req StatusRequest
	if !readJSON(w, r, &req) {
		return
	}
	m, err := a.svc.SetMilestoneStatus(r.Context(), id, auth.MustUser(r.Context()).ID, req.Status)
	if err != nil {
		httpx.Error(w, err, "The milestone could not be changed.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectMilestone(m))
}

func (a *API) deleteMilestone(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "milestoneID")
	if !ok {
		return
	}
	if err := a.svc.DeleteMilestone(r.Context(), id, auth.MustUser(r.Context()).ID); err != nil {
		httpx.Error(w, err, "The milestone could not be deleted.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeDetail answers with the goal as its detail page shows it: milestones
// and the recent progress notes, read fresh after whatever changed.
func (a *API) writeDetail(w http.ResponseWriter, r *http.Request, status int, id uuid.UUID) {
	userID := auth.MustUser(r.Context()).ID
	goal, err := a.svc.Get(r.Context(), id, userID)
	if err != nil {
		httpx.Error(w, err, "The goal could not be loaded.")
		return
	}
	milestones, err := a.svc.Milestones(r.Context(), id, userID)
	if err != nil {
		httpx.Error(w, err, "The goal could not be loaded.")
		return
	}
	updates, err := a.svc.Updates(r.Context(), id, userID, 20)
	if err != nil {
		httpx.Error(w, err, "The goal could not be loaded.")
		return
	}
	goal = goal.WithMilestones(milestones)

	out := GoalDetail{
		GoalSummary: summarize(goal),
		Motivation:  goal.Motivation, Success: goal.Success,
		Milestones: make([]MilestoneView, 0, len(milestones)),
		Updates:    make([]GoalUpdateView, 0, len(updates)),
	}
	for _, m := range milestones {
		out.Milestones = append(out.Milestones, projectMilestone(m))
	}
	for _, u := range updates {
		out.Updates = append(out.Updates, projectUpdate(u))
	}
	if len(updates) > 0 {
		latest := projectUpdate(updates[0])
		out.LatestUpdate = &latest
	}
	httpx.WriteJSON(w, status, out)
}

func summarize(g Goal) GoalSummary {
	out := GoalSummary{
		ID: g.ID, Title: g.Title, Category: g.Category, Status: g.Status,
		TargetDate: formatDate(g.TargetDate), MilestoneTotal: g.MilestoneTotal, MilestoneDone: g.MilestoneDone,
		CreatedAt: g.CreatedAt,
	}
	if g.LatestUpdate != nil {
		u := projectUpdate(*g.LatestUpdate)
		out.LatestUpdate = &u
	}
	return out
}

func projectUpdate(u Update) GoalUpdateView {
	return GoalUpdateView{ID: u.ID, Note: u.Note, Progress: u.Progress, CreatedAt: u.CreatedAt}
}

func projectMilestone(m Milestone) MilestoneView {
	return MilestoneView{ID: m.ID, Title: m.Title, Status: m.Status, TargetDate: formatDate(m.TargetDate), CompletedAt: m.CompletedAt}
}

func formatDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(dateLayout)
}

func readGoal(w http.ResponseWriter, r *http.Request) (Input, bool) {
	var req GoalRequest
	if !readJSON(w, r, &req) {
		return Input{}, false
	}
	target, ok := parseDate(w, req.TargetDate)
	if !ok {
		return Input{}, false
	}
	return Input{Title: req.Title, Motivation: req.Motivation, Success: req.Success, Category: req.Category, TargetDate: target}, true
}

func readMilestone(w http.ResponseWriter, r *http.Request) (MilestoneInput, bool) {
	var req MilestoneRequest
	if !readJSON(w, r, &req) {
		return MilestoneInput{}, false
	}
	target, ok := parseDate(w, req.TargetDate)
	if !ok {
		return MilestoneInput{}, false
	}
	return MilestoneInput{Title: req.Title, TargetDate: target}, true
}

// parseDate reads an optional calendar day. Unlike the web form, which drops
// a date it cannot read, the API says so: a client sending a malformed date
// has a bug worth hearing about.
func parseDate(w http.ResponseWriter, raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, true
	}
	t, err := time.Parse(dateLayout, raw)
	if err != nil {
		httpx.Error(w, apperr.FieldErrors{}.Add("targetDate", "Use a date like 2026-12-31."), "Use a date like 2026-12-31.")
		return time.Time{}, false
	}
	return t, true
}

func pathID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return uuid.Nil, false
	}
	return id, true
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := httpx.ReadJSON(w, r, dst, httpx.ReadOptions{MaxBytes: 16 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return false
	}
	return true
}
