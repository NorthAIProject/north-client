package lifts

import (
	"net/http"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
)

// API is lifting for native clients: a set logged as it is done, the last
// workout's numbers to start from, and the stats.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/lifts/stats", a.stats)
	r.Get("/lifts/last", a.last)
	r.Post("/lifts/sets", a.log)
	r.Delete("/lifts/sets/{setID}", a.undo)
	r.Get("/lifts/sessions/{sessionID}/recap", a.recap)
}

type LiftSetRequest struct {
	ExerciseName      string     `json:"exerciseName"`
	ExerciseSlug      string     `json:"exerciseSlug,omitempty"`
	SetNumber         int        `json:"setNumber"`
	WeightKg          float64    `json:"weightKg"`
	Reps              int        `json:"reps"`
	PerformedAt       *time.Time `json:"performedAt,omitempty"`
	ActivitySessionID *uuid.UUID `json:"activitySessionId,omitempty"`
}

type LiftSetView struct {
	ID                uuid.UUID  `json:"id"`
	ExerciseKey       string     `json:"exerciseKey"`
	ExerciseName      string     `json:"exerciseName"`
	ExerciseSlug      string     `json:"exerciseSlug"`
	SetNumber         int        `json:"setNumber"`
	WeightKg          float64    `json:"weightKg"`
	Reps              int        `json:"reps"`
	E1RMKg            float64    `json:"e1rmKg"`
	PerformedAt       time.Time  `json:"performedAt"`
	ActivitySessionID *uuid.UUID `json:"activitySessionId,omitempty"`
}

type LiftLastExercise struct {
	Key         string        `json:"key"`
	Name        string        `json:"name"`
	PerformedOn string        `json:"performedOn"`
	Sets        []LiftSetView `json:"sets"`
}

type LiftLastView struct {
	Exercises []LiftLastExercise `json:"exercises"`
}

type LiftDayValue struct {
	Date  string  `json:"date"`
	Value float64 `json:"value"`
}

type LiftExerciseStats struct {
	Key          string         `json:"key"`
	Name         string         `json:"name"`
	Slug         string         `json:"slug"`
	Sets         int            `json:"sets"`
	Reps         int            `json:"reps"`
	VolumeKg     float64        `json:"volumeKg"`
	BestWeightKg float64        `json:"bestWeightKg"`
	BestE1RMKg   float64        `json:"bestE1rmKg"`
	LastOn       string         `json:"lastOn"`
	Trend        []LiftDayValue `json:"trend"`
}

type LiftRecordView struct {
	ExerciseName   string  `json:"exerciseName"`
	Date           string  `json:"date"`
	WeightKg       float64 `json:"weightKg"`
	Reps           int     `json:"reps"`
	E1RMKg         float64 `json:"e1rmKg"`
	PreviousE1RMKg float64 `json:"previousE1rmKg"`
}

type LiftMuscleSets struct {
	Muscle string `json:"muscle"`
	Sets   int    `json:"sets"`
}

type LiftStatsView struct {
	Range         string              `json:"range"`
	Workouts      int                 `json:"workouts"`
	Sets          int                 `json:"sets"`
	Reps          int                 `json:"reps"`
	VolumeKg      float64             `json:"volumeKg"`
	PriorVolumeKg float64             `json:"priorVolumeKg"`
	Exercises     []LiftExerciseStats `json:"exercises"`
	Records       []LiftRecordView    `json:"records"`
	Weekly        []LiftDayValue      `json:"weekly"`
	Muscles       []LiftMuscleSets    `json:"muscles"`
}

// LiftRecapExercise is one movement of a finished workout against the last
// time it was done. Previous fields are absent the first time.
type LiftRecapExercise struct {
	Name             string   `json:"name"`
	Sets             int      `json:"sets"`
	VolumeKg         float64  `json:"volumeKg"`
	Best             string   `json:"best,omitempty"`
	E1RMKg           float64  `json:"e1rmKg"`
	PreviousVolumeKg *float64 `json:"previousVolumeKg,omitempty"`
	PreviousE1RMKg   *float64 `json:"previousE1rmKg,omitempty"`
	ChangeE1RMKg     *float64 `json:"changeE1rmKg,omitempty"`
}

// LiftRecapView is a finished workout in one sentence and per exercise: what
// the finish screen, the plan page and Insights all show.
type LiftRecapView struct {
	SessionID       uuid.UUID           `json:"sessionId"`
	StartedAt       time.Time           `json:"startedAt"`
	PlanWeekday     string              `json:"planWeekday,omitempty"`
	Focus           string              `json:"focus,omitempty"`
	Sentence        string              `json:"sentence"`
	DurationSeconds int                 `json:"durationSeconds"`
	SetsDone        int                 `json:"setsDone"`
	SetsPrescribed  int                 `json:"setsPrescribed"`
	VolumeKg        float64             `json:"volumeKg"`
	Calories        float64             `json:"calories"`
	Exercises       []LiftRecapExercise `json:"exercises"`
}

func (a *API) recap(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "sessionID"))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return
	}
	recap, err := a.svc.Recap(r.Context(), auth.MustUser(r.Context()), id)
	if err != nil {
		httpx.Error(w, err, "That workout's recap could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ProjectRecap(recap))
}

// ProjectRecap is the wire shape of a recap.
func ProjectRecap(rc lift.Recap) LiftRecapView {
	out := LiftRecapView{
		SessionID: rc.SessionID, StartedAt: rc.StartedAt, PlanWeekday: rc.PlanWeekday, Focus: rc.Focus,
		Sentence: rc.Sentence, DurationSeconds: int(rc.Duration.Seconds()), SetsDone: rc.SetsDone,
		SetsPrescribed: rc.SetsPrescribed, VolumeKg: rc.VolumeKg, Calories: util.RoundHalfUpToScale(rc.Calories, 1),
		Exercises: make([]LiftRecapExercise, 0, len(rc.Exercises)),
	}
	for _, e := range rc.Exercises {
		row := LiftRecapExercise{Name: e.Name, Sets: e.Sets, VolumeKg: e.VolumeKg, Best: e.Best, E1RMKg: e.E1RM}
		if e.HasPrevious {
			prevVolume, prevE1RM, change := e.PreviousVolume, e.PreviousE1RM, e.Change
			row.PreviousVolumeKg, row.PreviousE1RMKg, row.ChangeE1RMKg = &prevVolume, &prevE1RM, &change
		}
		out.Exercises = append(out.Exercises, row)
	}
	return out
}

func (a *API) stats(w http.ResponseWriter, r *http.Request) {
	user := auth.MustUser(r.Context())
	st, err := a.svc.Stats(r.Context(), user, timerange.Parse(r.URL.Query().Get("range"), user.Location()))
	if err != nil {
		httpx.Error(w, err, "Lifting stats could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ProjectStats(st))
}

func (a *API) last(w http.ResponseWriter, r *http.Request) {
	keys := r.URL.Query()["exercise"]
	last, err := a.svc.Last(r.Context(), auth.MustUser(r.Context()), keys)
	if err != nil {
		httpx.Error(w, err, "The last workout could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ProjectLast(keys, last))
}

func (a *API) log(w http.ResponseWriter, r *http.Request) {
	var req LiftSetRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 4 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return
	}
	set, err := a.svc.Log(r.Context(), auth.MustUser(r.Context()), LogInput(req))
	if err != nil {
		httpx.Error(w, err, "The set could not be logged.")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, ProjectSet(set))
}

func (a *API) undo(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "setID"))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "No such set.")
		return
	}
	if err := a.svc.Undo(r.Context(), auth.MustUser(r.Context()), id); err != nil {
		httpx.Error(w, err, "That set could not be removed.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func ProjectSet(s Set) LiftSetView {
	return LiftSetView{
		ID: s.ID, ExerciseKey: s.Key(), ExerciseName: s.ExerciseName, ExerciseSlug: s.ExerciseSlug,
		SetNumber: s.SetNumber, WeightKg: s.WeightKg, Reps: s.Reps, E1RMKg: util.RoundHalfUpToScale(s.E1RM(), 1),
		PerformedAt: s.PerformedAt, ActivitySessionID: s.ActivitySessionID,
	}
}

// ProjectLast answers in the order the keys were asked for, leaving out
// exercises never done.
func ProjectLast(keys []string, last map[string][]Set) LiftLastView {
	out := LiftLastView{Exercises: []LiftLastExercise{}}
	seen := map[string]bool{}
	for _, k := range keys {
		key := lift.KeyFor("", k)
		sets, ok := last[key]
		if !ok || seen[key] || len(sets) == 0 {
			continue
		}
		seen[key] = true
		ex := LiftLastExercise{Key: key, Name: sets[len(sets)-1].ExerciseName, PerformedOn: sets[0].LogDate.Format(time.DateOnly)}
		for _, s := range sets {
			ex.Sets = append(ex.Sets, ProjectSet(s))
		}
		out.Exercises = append(out.Exercises, ex)
	}
	return out
}

func ProjectStats(st Stats) LiftStatsView {
	out := LiftStatsView{
		Range:         st.Range.Key,
		Workouts:      lift.Workouts(st.Sets),
		Sets:          len(st.Sets),
		VolumeKg:      st.VolumeKg(),
		PriorVolumeKg: st.PriorVolumeKg,
		Exercises:     []LiftExerciseStats{},
		Records:       []LiftRecordView{},
		Weekly:        []LiftDayValue{},
		Muscles:       []LiftMuscleSets{},
	}
	for _, s := range st.Sets {
		out.Reps += s.Reps
	}
	for _, e := range st.Exercises {
		v := LiftExerciseStats{
			Key: e.Key, Name: e.Name, Slug: e.Slug, Sets: e.Sets, Reps: e.Reps, VolumeKg: e.VolumeKg,
			BestWeightKg: e.BestWeightKg, BestE1RMKg: e.BestE1RM, LastOn: e.LastOn.Format(time.DateOnly),
			Trend: days(e.Trend),
		}
		out.Exercises = append(out.Exercises, v)
	}
	for _, r := range st.Records {
		out.Records = append(out.Records, LiftRecordView{
			ExerciseName: r.Set.ExerciseName, Date: r.Set.LogDate.Format(time.DateOnly),
			WeightKg: r.Set.WeightKg, Reps: r.Set.Reps, E1RMKg: r.E1RM, PreviousE1RMKg: r.Previous,
		})
	}
	out.Weekly = days(st.Weekly)
	for _, m := range st.Muscles {
		out.Muscles = append(out.Muscles, LiftMuscleSets(m))
	}
	return out
}

func days(values []lift.DayValue) []LiftDayValue {
	out := make([]LiftDayValue, len(values))
	for i, v := range values {
		out[i] = LiftDayValue{Date: v.Day.Format(time.DateOnly), Value: v.Value}
	}
	return out
}
