package lifts

import (
	"bytes"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/bodymap"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/shared/middleware"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
)

// MaxImportBytes caps a Hevy export. Years of training are a few megabytes.
const MaxImportBytes = 10 << 20

// API is lifting for native clients: a set logged as it is done, the last
// workout's numbers to start from, and the stats.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Register(g *echo.Group) {
	g.GET("/lifts/stats", a.stats)
	g.GET("/lifts/last", a.last)
	g.GET("/lifts/today", a.today)
	g.POST("/lifts/sets", a.log)
	g.DELETE("/lifts/sets/:setID", a.undo)
	g.GET("/lifts/sessions/:sessionID/recap", a.recap)
	g.GET("/body/map", a.bodyMap)
}

// WorkoutCardView is a finished workout for someone else to read. It carries
// no calories, heart rate or body numbers.
type WorkoutCardView struct {
	SessionID       uuid.UUID             `json:"sessionId"`
	Name            string                `json:"name"`
	ActivityCode    string                `json:"activityCode"`
	StartedAt       time.Time             `json:"startedAt"`
	EndedAt         time.Time             `json:"endedAt"`
	DurationSeconds int                   `json:"durationSeconds"`
	DistanceM       *float64              `json:"distanceM,omitempty"`
	Sets            int                   `json:"sets"`
	VolumeKg        float64               `json:"volumeKg"`
	PRs             int                   `json:"prs"`
	Exercises       []WorkoutExerciseView `json:"exercises"`
}

// WorkoutExerciseView is one movement on a card.
type WorkoutExerciseView struct {
	Name     string  `json:"name"`
	Sets     int     `json:"sets"`
	VolumeKg float64 `json:"volumeKg"`
	// Best is the heaviest set, "22.5 kg × 8"; absent for bodyweight.
	Best string `json:"best,omitempty"`
	PR   bool   `json:"pr"`
}

// ProjectCard keeps the first limit exercises (limit <= 0 keeps all) and
// always emits exercises as [] rather than null.
func ProjectCard(c lift.Card, limit int) WorkoutCardView {
	ex := c.Exercises
	if limit > 0 && len(ex) > limit {
		ex = ex[:limit]
	}
	out := WorkoutCardView{
		SessionID: c.ID, Name: c.Name, ActivityCode: c.ActivityCode,
		StartedAt: c.StartedAt, EndedAt: c.EndedAt, DurationSeconds: int(c.Duration.Seconds()),
		DistanceM: c.DistanceM, Sets: c.Sets, VolumeKg: c.VolumeKg, PRs: c.PRs,
		Exercises: make([]WorkoutExerciseView, 0, len(ex)),
	}
	for _, e := range ex {
		out.Exercises = append(out.Exercises, WorkoutExerciseView{Name: e.Name, Sets: e.Sets, VolumeKg: e.VolumeKg, Best: e.Best, PR: e.PR})
	}
	return out
}

// BodyMapView is the body figure's payload. Muscles lists every region of the
// figure, untrained ones at intensity 0, so a client needs no region list of
// its own. Ids are open strings: a figure that gains a region must not break
// a client that shipped before it.
type BodyMapView struct {
	Days int `json:"days"`
	// LastSessionOn is the date of the latest working set, in the person's
	// timezone; absent when nothing was ever logged.
	LastSessionOn string          `json:"lastSessionOn,omitempty"`
	Muscles       []BodyMapMuscle `json:"muscles"`
}

type BodyMapMuscle struct {
	ID        string  `json:"id"`
	Intensity float64 `json:"intensity"`
	// LastTrainedOn is absent for a region never trained.
	LastTrainedOn string `json:"lastTrainedOn,omitempty"`
}

// bodyMap is GET /body/map?days=7: which regions recent training heated.
func (a *API) bodyMap(c *echo.Context) error {
	ctx := c.Request().Context()
	days := 7
	if raw := c.QueryParam("days"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			msg := i18n.T(ctx, "lifts.api.days")
			return httpx.Fail(apperr.FieldErrors{}.Add("days", msg), msg)
		}
		days = n
	}
	user := auth.MustUser(ctx)
	m, err := a.svc.BodyMap(ctx, user, days)
	if err != nil {
		return httpx.Fail(localize(ctx, err), i18n.T(ctx, "lifts.api.bodymap"))
	}
	return c.JSON(http.StatusOK, NewBodyMapView(m, user.Location()))
}

// NewBodyMapView renders a body map with dates in loc.
func NewBodyMapView(m bodymap.Map, loc *time.Location) BodyMapView {
	out := BodyMapView{Days: m.Days, Muscles: make([]BodyMapMuscle, 0, len(m.Regions))}
	if !m.LastSession.IsZero() {
		out.LastSessionOn = m.LastSession.In(loc).Format(time.DateOnly)
	}
	for _, region := range m.Regions {
		muscle := BodyMapMuscle{ID: region.Region, Intensity: math.Round(region.Intensity*1000) / 1000}
		if !region.LastTrained.IsZero() {
			muscle.LastTrainedOn = region.LastTrained.In(loc).Format(time.DateOnly)
		}
		out.Muscles = append(out.Muscles, muscle)
	}
	return out
}

// RegisterUploads registers on the API's upload group, whose body cap fits a
// file.
func (a *API) RegisterUploads(g *echo.Group) {
	g.POST("/lifts/import/hevy", a.importHevy)
}

// LiftImportView is what an import did.
type LiftImportView struct {
	Workouts   int      `json:"workouts"`
	Duplicates int      `json:"duplicates"`
	Sets       int      `json:"sets"`
	Skipped    int      `json:"skipped"`
	Unmatched  []string `json:"unmatched"`
}

// importHevy takes a Hevy workout export as the multipart field "file".
func (a *API) importHevy(c *echo.Context) error {
	r := c.Request()
	ctx := r.Context()
	r.Body = http.MaxBytesReader(c.Response(), r.Body, MaxImportBytes+(1<<20))
	if err := r.ParseMultipartForm(4 << 20); err != nil {
		msg := i18n.T(ctx, "lifts.err.upload")
		var tooBig *http.MaxBytesError
		if apperr.As(err, &tooBig) {
			msg = i18n.T(ctx, "lifts.err.toolarge")
		}
		return httpx.Fail(apperr.FieldErrors{}.Add("file", msg), msg)
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()
	data, msgKey := formFile(r)
	if msgKey != "" {
		msg := i18n.T(ctx, msgKey)
		return httpx.Fail(apperr.FieldErrors{}.Add("file", msg), msg)
	}
	res, err := a.svc.ImportHevy(ctx, auth.MustUser(ctx), bytes.NewReader(data))
	if err != nil {
		err = localize(ctx, err)
		if httpx.Status(err) < http.StatusInternalServerError {
			return httpx.Fail(err, i18n.T(ctx, "lifts.api.import"))
		}
		// Each set is saved as it goes, so what res counts was kept.
		middleware.FromContext(ctx).Error("import hevy", slog.Any("error", err))
		return c.JSON(http.StatusInternalServerError, ProjectImportPartial(i18n.T(ctx, "lifts.err.partial"), res))
	}
	return c.JSON(http.StatusOK, projectImport(res))
}

// LiftImportPartial is the 500 for an import that stopped part way: the usual
// error shape, plus what was saved before it stopped.
type LiftImportPartial struct {
	Error    httpx.ErrorDetail `json:"error"`
	Imported LiftImportView    `json:"imported"`
}

// ProjectImportPartial is the reply for an import that failed after res.
func ProjectImportPartial(message string, res ImportResult) LiftImportPartial {
	return LiftImportPartial{
		Error:    httpx.ErrorDetail{Message: message, Code: "import_partial"},
		Imported: projectImport(res),
	}
}

func projectImport(res ImportResult) LiftImportView {
	unmatched := res.Unmatched
	if unmatched == nil {
		unmatched = []string{}
	}
	return LiftImportView{
		Workouts: res.Workouts, Duplicates: res.Duplicates, Sets: res.Sets, Skipped: res.Skipped, Unmatched: unmatched,
	}
}

type LiftSetRequest struct {
	ExerciseName      string     `json:"exerciseName"`
	ExerciseSlug      string     `json:"exerciseSlug,omitempty"`
	SetNumber         int        `json:"setNumber"`
	WeightKg          float64    `json:"weightKg"`
	Reps              int        `json:"reps"`
	PerformedAt       *time.Time `json:"performedAt,omitempty"`
	ActivitySessionID *uuid.UUID `json:"activitySessionId,omitempty"`
	Kind              string     `json:"kind,omitempty"`
	RIR               *int       `json:"rir,omitempty"`
	// ClientID makes a retried upload safe: the same id again answers with
	// the set already logged.
	ClientID *uuid.UUID `json:"clientId,omitempty"`
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
	Kind              string     `json:"kind"`
	RIR               *int       `json:"rir,omitempty"`
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

func (a *API) recap(c *echo.Context) error {
	ctx := c.Request().Context()
	id, err := uuid.Parse(c.Param("sessionID"))
	if err != nil {
		return httpx.Fail(apperr.ErrNotFound, i18n.T(ctx, "lifts.api.notfound"))
	}
	recap, err := a.svc.Recap(ctx, auth.MustUser(ctx), id)
	if err != nil {
		return httpx.Fail(err, i18n.T(ctx, "lifts.api.recap"))
	}
	view := ProjectRecap(recap)
	view.Sentence = recap.SentenceIn(i18n.LocaleFrom(ctx))
	return c.JSON(http.StatusOK, view)
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

func (a *API) stats(c *echo.Context) error {
	ctx := c.Request().Context()
	user := auth.MustUser(ctx)
	st, err := a.svc.Stats(ctx, user, timerange.Parse(c.QueryParam("range"), user.Location()))
	if err != nil {
		return httpx.Fail(err, i18n.T(ctx, "lifts.api.stats"))
	}
	return c.JSON(http.StatusOK, ProjectStats(st))
}

func (a *API) last(c *echo.Context) error {
	ctx := c.Request().Context()
	keys := c.QueryParams()["exercise"]
	last, err := a.svc.Last(ctx, auth.MustUser(ctx), keys)
	if err != nil {
		return httpx.Fail(err, i18n.T(ctx, "lifts.api.last"))
	}
	return c.JSON(http.StatusOK, ProjectLast(keys, last))
}

// LiftTodayView is the web's set logger: the sets logged since local
// midnight, oldest first, and the names of exercises done in the last
// fortnight, most recent first, to offer as suggestions.
type LiftTodayView struct {
	Sets   []LiftSetView `json:"sets"`
	Recent []string      `json:"recent"`
}

func (a *API) today(c *echo.Context) error {
	ctx := c.Request().Context()
	sets, recent, err := a.svc.Today(ctx, auth.MustUser(ctx))
	if err != nil {
		return httpx.Fail(err, i18n.T(ctx, "lifts.err.generic"))
	}
	out := LiftTodayView{Sets: make([]LiftSetView, 0, len(sets)), Recent: recent}
	if out.Recent == nil {
		out.Recent = []string{}
	}
	for _, s := range sets {
		out.Sets = append(out.Sets, ProjectSet(s))
	}
	return c.JSON(http.StatusOK, out)
}

func (a *API) log(c *echo.Context) error {
	ctx := c.Request().Context()
	var req LiftSetRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 4 << 10}, i18n.T(ctx, "lifts.api.body")); err != nil {
		return err
	}
	user := auth.MustUser(ctx)
	// No set number is the web form's set: the server matches the name to
	// the catalogue and numbers it as the next set of that exercise today,
	// as the web handler always has.
	if req.SetNumber == 0 && strings.TrimSpace(req.ExerciseName) != "" {
		req.ExerciseName = strings.TrimSpace(req.ExerciseName)
		if req.ExerciseSlug == "" {
			slug, err := a.svc.MatchExercise(ctx, req.ExerciseName)
			if err != nil {
				return httpx.Fail(err, i18n.T(ctx, "lifts.api.log"))
			}
			req.ExerciseSlug = slug
		}
		n, err := a.svc.NextSetNumber(ctx, user, req.ExerciseSlug, req.ExerciseName)
		if err != nil {
			return httpx.Fail(err, i18n.T(ctx, "lifts.api.log"))
		}
		req.SetNumber = n
	}
	set, err := a.svc.Log(ctx, user, LogInput(req))
	if err != nil {
		return httpx.Fail(localize(ctx, err), i18n.T(ctx, "lifts.api.log"))
	}
	return c.JSON(http.StatusCreated, ProjectSet(set))
}

func (a *API) undo(c *echo.Context) error {
	ctx := c.Request().Context()
	id, err := uuid.Parse(c.Param("setID"))
	if err != nil {
		return httpx.Fail(apperr.ErrNotFound, i18n.T(ctx, "lifts.api.nosuch"))
	}
	if err := a.svc.Undo(ctx, auth.MustUser(ctx), id); err != nil {
		return httpx.Fail(err, i18n.T(ctx, "lifts.api.remove"))
	}
	return c.NoContent(http.StatusNoContent)
}

func ProjectSet(s Set) LiftSetView {
	return LiftSetView{
		ID: s.ID, ExerciseKey: s.Key(), ExerciseName: s.ExerciseName, ExerciseSlug: s.ExerciseSlug,
		SetNumber: s.SetNumber, WeightKg: s.WeightKg, Reps: s.Reps, E1RMKg: util.RoundHalfUpToScale(s.E1RM(), 1),
		PerformedAt: s.PerformedAt, ActivitySessionID: s.ActivitySessionID, Kind: kindOrWork(s.Kind), RIR: s.RIR,
	}
}

// kindOrWork names a set's kind on the wire. A set built in code without one
// is a work set, as every set before kinds existed was.
func kindOrWork(kind string) string {
	if kind == "" {
		return lift.KindWork
	}
	return kind
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

// formFile reads the upload the CSRF middleware has already parsed, or
// names the catalogue key of what is wrong with it.
func formFile(r *http.Request) ([]byte, string) {
	file, _, err := r.FormFile("file")
	if err != nil {
		return nil, "lifts.err.nofile"
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, MaxImportBytes+1))
	if err != nil {
		return nil, "lifts.err.upload"
	}
	if len(data) > MaxImportBytes {
		return nil, "lifts.err.toolarge"
	}
	return data, ""
}
