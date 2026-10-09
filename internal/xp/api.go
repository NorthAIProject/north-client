package xp

import (
	"net/http"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is XP, levels and the friends leaderboard for native clients. Mount
// behind auth.RequireBearer.
type API struct {
	svc *Service
}

func NewAPI(svc *Service) *API { return &API{svc: svc} }

func (a *API) Routes(r chi.Router) {
	r.Get("/xp", a.summary)
	r.Get("/leaderboard", a.board)
}

type LevelView struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	// Floor is the XP this level starts at; Next the XP the next one does,
	// absent at the top level.
	Floor int  `json:"floor"`
	Next  *int `json:"next,omitempty"`
}

type EarnedView struct {
	Kind   string `json:"kind"`
	Count  int    `json:"count"`
	Points int    `json:"points"`
}

type SummaryView struct {
	Total     int       `json:"total"`
	Level     LevelView `json:"level"`
	WeekTotal int       `json:"weekTotal"`
	// Week holds only the five kinds the first clients shipped with: they
	// decode kind as a closed enum, so one more kind would fail the whole
	// response. WeekTotal still counts every kind.
	Week []EarnedView `json:"week"`
	// Earned is this week by every kind. Its kind is open-ended, so a client
	// shows a label it does not know rather than failing.
	Earned []EarnedView `json:"earned"`
}

// firstKinds are the kinds Week is limited to.
var firstKinds = map[string]bool{
	KindWorkout: true, KindHabitKept: true, KindStreakDay: true, KindMilestone: true, KindGoal: true,
}

type EntryView struct {
	Rank        int        `json:"rank"`
	UserID      uuid.UUID  `json:"userId"`
	DisplayName string     `json:"displayName"`
	Handle      string     `json:"handle"`
	Value       int        `json:"value"`
	Level       *LevelView `json:"level,omitempty"`
	Me          bool       `json:"me"`
}

type BoardView struct {
	Metric string `json:"metric"`
	// Period is week or all for xp, week for workouts, absent for streak.
	Period  string      `json:"period,omitempty"`
	Entries []EntryView `json:"entries"`
	Sharing bool        `json:"sharing"`
}

func (a *API) summary(w http.ResponseWriter, r *http.Request) {
	s, err := a.svc.Summary(r.Context(), auth.MustUser(r.Context()), time.Now())
	if err != nil {
		httpx.Error(w, err, "Your XP could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectSummary(s))
}

func (a *API) board(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	metric := q.Get("metric")
	if metric == "" {
		metric = MetricXP
	}
	b, err := a.svc.Board(r.Context(), auth.MustUser(r.Context()), metric, q.Get("period"), time.Now())
	if err != nil {
		httpx.Error(w, err, "The leaderboard could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectBoard(b))
}

func projectLevel(l Level) LevelView {
	v := LevelView{Number: l.Number, Title: l.Title, Floor: l.Floor}
	if l.Next > 0 {
		v.Next = util.Ptr(l.Next)
	}
	return v
}

func projectSummary(s Summary) SummaryView {
	out := SummaryView{
		Total: s.Total, Level: projectLevel(s.Level), WeekTotal: s.WeekTotal,
		Week: make([]EarnedView, 0, len(firstKinds)), Earned: make([]EarnedView, 0, len(s.Week)),
	}
	for _, e := range s.Week {
		out.Earned = append(out.Earned, EarnedView(e))
		if firstKinds[e.Kind] {
			out.Week = append(out.Week, EarnedView(e))
		}
	}
	return out
}

func projectBoard(b Board) BoardView {
	out := BoardView{Metric: b.Metric, Period: b.Period, Sharing: b.Sharing, Entries: make([]EntryView, 0, len(b.Entries))}
	for _, e := range b.Entries {
		v := EntryView{
			Rank: e.Rank, UserID: e.UserID, DisplayName: e.DisplayName, Handle: e.Handle, Value: e.Value, Me: e.Me,
		}
		if e.Level != nil {
			v.Level = util.Ptr(projectLevel(*e.Level))
		}
		out.Entries = append(out.Entries, v)
	}
	return out
}
