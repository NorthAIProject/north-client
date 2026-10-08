package workouts

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/workouts/plan"
	workoutpages "github.com/NorthAIProject/north-client/web/workouts"
)

// The week card on the followed plan's page. ?week=next addresses next week.

func (h *Handler) showWeek(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	progress, err := h.weekFor(r.Context(), user, wantsNextWeek(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	view, err := h.weekView(r.Context(), user, progress, wantsNextWeek(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	render(w, r, http.StatusOK, workoutpages.WeekCard(view))
}

// editWeek opens the editor on the week as it stands. Days already filled
// from another plan stay pinned to that session; the rest follow the rotation.
func (h *Handler) editWeek(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	progress, err := h.weekFor(r.Context(), user, wantsNextWeek(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	view, err := h.weekView(r.Context(), user, progress, wantsNextWeek(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	view.Days = stillOpen(view.Days, view.Today)
	render(w, r, http.StatusOK, workoutpages.WeekEditor(view))
}

// stillOpen drops the days a change would drop: past ones never trained.
func stillOpen(days []workoutpages.WeekDayView, today int) []workoutpages.WeekDayView {
	var out []workoutpages.WeekDayView
	for _, d := range days {
		if d.Completed || plan.WeekPosition(d.Weekday) >= today {
			out = append(out, d)
		}
	}
	return out
}

// previewWeek answers every change in the editor with the week it would make.
func (h *Handler) previewWeek(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	change, err := weekChangeFromForm(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	progress, err := h.svc.SuggestWeek(r.Context(), user, time.Now(), change)
	message := ""
	if apperr.Is(err, apperr.ErrValidation) {
		// Show the editor again with the reason rather than a bare error.
		message = validationMessage(err)
		progress, err = h.weekFor(r.Context(), user, change.NextWeek)
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	view, err := h.weekView(r.Context(), user, progress, change.NextWeek)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	view.Error = message
	if change.PlanID != nil {
		view.Source = *change.PlanID
	}
	view.Matching = matchingExcept(view.Plans, len(view.Days), view.Source)
	pinFromForm(&view, change)
	render(w, r, http.StatusOK, workoutpages.WeekEditor(view))
}

func (h *Handler) saveWeek(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	change, err := weekChangeFromForm(r)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	progress, err := h.svc.SetWeek(r.Context(), user, time.Now(), change)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	view, err := h.weekView(r.Context(), user, progress, change.NextWeek)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	render(w, r, http.StatusOK, workoutpages.WeekCard(view))
}

func (h *Handler) resetWeek(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	progress, err := h.svc.ResetWeek(r.Context(), user, time.Now(), wantsNextWeek(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	view, err := h.weekView(r.Context(), user, progress, wantsNextWeek(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	render(w, r, http.StatusOK, workoutpages.WeekCard(view))
}

func (h *Handler) activatePlan(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, apperr.ErrNotFound)
		return
	}
	active, err := h.svc.SetActivePlan(r.Context(), auth.MustUser(r.Context()), id)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/app/training/"+active.ID.String(), http.StatusSeeOther)
}

func (h *Handler) weekFor(ctx context.Context, user users.User, nextWeek bool) (WeekProgress, error) {
	if nextWeek {
		return h.svc.NextWeek(ctx, user, time.Now())
	}
	return h.svc.WeekProgress(ctx, user, time.Now())
}

// weekView is a week as the page draws it, with what the editor needs to
// change it.
func (h *Handler) weekView(ctx context.Context, user users.User, progress WeekProgress, nextWeek bool) (workoutpages.WeekView, error) {
	plans, err := h.svc.ListCurrentPlans(ctx, user.ID, 50)
	if err != nil {
		return workoutpages.WeekView{}, err
	}
	view := workoutpages.WeekView{NextWeek: nextWeek, Start: progress.Start, Custom: progress.Custom, Today: -1, Render: renderID()}
	if !nextWeek {
		view.Today = plan.WeekPosition(time.Now().In(user.Location()).Weekday().String())
	}
	var activeIntake uuid.UUID
	if len(plans) > 0 {
		// ListCurrentPlans puts the followed plan first.
		view.Source, activeIntake = plans[0].ID, plans[0].IntakeID
	}
	for _, p := range plans {
		view.Plans = append(view.Plans, workoutpages.PlanSummary{ID: p.ID, Name: p.Plan.Name, Weeks: p.Plan.WeeksTotal, Days: len(p.Plan.Days), CreatedAt: p.CreatedAt})
		for i, d := range p.Plan.Days {
			view.Sessions = append(view.Sessions, workoutpages.SessionOption{
				Value: workoutpages.SessionValue(p.ID, i),
				Label: fmt.Sprintf("%s · %s", p.Plan.Name, d.Focus),
			})
		}
	}
	for _, d := range progress.Days {
		day := workoutpages.WeekDayView{
			Weekday: d.Weekday, Date: d.Date, Focus: d.Day.Focus, PlanName: d.PlanName,
			FromOtherPlan: d.IntakeID != activeIntake,
			Completed:     d.Completed,
			IsNext:        progress.HasNext && progress.NextDay.Date.Equal(d.Date),
			StartTime:     d.Day.StartTime,
		}
		if day.FromOtherPlan {
			day.Pinned = workoutpages.SessionValue(d.PlanID, d.DayIndex)
		}
		view.Days = append(view.Days, day)
	}
	view.Matching = matchingExcept(view.Plans, len(view.Days), view.Source)
	return view, nil
}

// renderID is a short random id for one rendering of the editor; see
// WeekView.Render.
func renderID() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// pinFromForm keeps the editor's own choices on re-render: a day pinned by
// hand stays pinned, and every other day shows the rotation's session.
func pinFromForm(view *workoutpages.WeekView, change WeekChange) {
	for i := range view.Days {
		view.Days[i].Pinned = ""
		if ref, ok := change.Assign[view.Days[i].Weekday]; ok {
			view.Days[i].Pinned = workoutpages.SessionValue(ref.PlanID, ref.DayIndex)
		}
	}
}

// matchingExcept are the plans written for exactly n days, other than source.
func matchingExcept(plans []workoutpages.PlanSummary, n int, source uuid.UUID) []workoutpages.PlanSummary {
	var out []workoutpages.PlanSummary
	for _, p := range plans {
		if p.Days == n && n > 0 && p.ID != source {
			out = append(out, p)
		}
	}
	return out
}

// weekChangeFromForm reads the editor: weekday (repeated), days (a suggested
// number, which replaces the weekdays), plan, and session_<Weekday> as
// "<planID>:<dayIndex>" for a day pinned to a session.
func weekChangeFromForm(r *http.Request) (WeekChange, error) {
	if err := r.ParseForm(); err != nil {
		return WeekChange{}, apperr.Wrap(apperr.ErrValidation, "read week form: %v", err)
	}
	change := WeekChange{NextWeek: wantsNextWeek(r)}
	if id, err := uuid.Parse(r.PostForm.Get("plan")); err == nil {
		change.PlanID = &id
	}
	if days := strings.TrimSpace(r.PostForm.Get("days")); days != "" {
		// A number of days asks for a fresh suggestion: the ticked weekdays
		// and pinned sessions belong to the week being replaced.
		change.Days = atoiOr(days, 0)
		return change, nil
	}
	change.Weekdays = r.PostForm["weekday"]
	chosen := make(map[string]bool, len(change.Weekdays))
	for _, wd := range change.Weekdays {
		if canonical, ok := plan.CanonicalWeekday(wd); ok {
			chosen[canonical] = true
		}
	}
	for key, values := range r.PostForm {
		weekday, ok := strings.CutPrefix(key, "session_")
		if !ok || !chosen[weekday] || len(values) == 0 || values[0] == "" {
			continue
		}
		ref, ok := parseSessionValue(values[0])
		if !ok {
			return WeekChange{}, apperr.Wrap(apperr.ErrValidation, "unknown session %q", values[0])
		}
		if change.Assign == nil {
			change.Assign = make(map[string]SessionRef)
		}
		change.Assign[weekday] = ref
	}
	return change, nil
}

func parseSessionValue(v string) (SessionRef, bool) {
	id, index, ok := strings.Cut(v, ":")
	if !ok {
		return SessionRef{}, false
	}
	planID, err := uuid.Parse(id)
	if err != nil {
		return SessionRef{}, false
	}
	dayIndex, err := strconv.Atoi(index)
	if err != nil {
		return SessionRef{}, false
	}
	return SessionRef{PlanID: planID, DayIndex: dayIndex}, true
}

// validationMessage is a validation error in the words it was raised with.
func validationMessage(err error) string {
	return strings.TrimSuffix(err.Error(), ": "+apperr.ErrValidation.Error())
}
