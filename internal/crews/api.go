package crews

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is crews for native clients. Mount behind auth.RequireBearer.
type API struct {
	svc     *Service
	siteURL string
}

func NewAPI(svc *Service, siteURL string) *API {
	return &API{svc: svc, siteURL: strings.TrimRight(siteURL, "/")}
}

func (a *API) Routes(r chi.Router) {
	r.Get("/crews", a.list)
	r.Post("/crews", a.create)
	r.Get("/crews/join/{code}", a.preview)
	r.Post("/crews/join/{code}", a.join)
	r.Get("/crews/{crewID}", a.board)
	r.Delete("/crews/{crewID}/members/{userID}", a.remove)
	r.Put("/crews/{crewID}/challenge", a.setChallenge)
	r.Delete("/crews/{crewID}/challenge", a.clearChallenge)
}

// JoinURL is the link that brings somebody into a crew.
func JoinURL(siteURL, code string) string { return strings.TrimRight(siteURL, "/") + "/i/c/" + code }

type CrewView struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	// MemberCount is how many are in it; the board lists them in members.
	MemberCount int    `json:"memberCount"`
	JoinURL     string `json:"joinUrl"`
}

type CrewList struct {
	Crews []CrewView `json:"crews"`
}

type ChallengeView struct {
	// Kind is checkins or workouts, so many a week.
	Kind   string `json:"kind"`
	Target int    `json:"target"`
}

type MemberView struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"displayName"`
	Handle      string    `json:"handle"`
	CheckedIn   bool      `json:"checkedIn"`
	WorkedOut   bool      `json:"workedOut"`
	Streak      int       `json:"streak"`
	// WeekProgress counts toward the challenge, Monday to Sunday in the
	// member's own time zone; 0 with no challenge.
	WeekProgress int  `json:"weekProgress"`
	Owner        bool `json:"owner"`
	Me           bool `json:"me"`
}

type BoardView struct {
	CrewView
	Challenge *ChallengeView `json:"challenge,omitempty"`
	Members   []MemberView   `json:"members"`
	WeekStart time.Time      `json:"weekStart"`
	IsOwner   bool           `json:"isOwner"`
}

type CreateRequest struct {
	Name string `json:"name"`
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	list, err := a.svc.Mine(r.Context(), auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "Crews could not be loaded.")
		return
	}
	out := CrewList{Crews: make([]CrewView, 0, len(list))}
	for _, c := range list {
		out.Crews = append(out.Crews, a.projectCrew(c))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

func (a *API) create(w http.ResponseWriter, r *http.Request) {
	var req CreateRequest
	if !readJSON(w, r, &req) {
		return
	}
	c, err := a.svc.Create(r.Context(), auth.MustUser(r.Context()).ID, req.Name)
	if err != nil {
		httpx.Error(w, err, "The crew could not be made.")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, a.projectCrew(c))
}

func (a *API) preview(w http.ResponseWriter, r *http.Request) {
	c, err := a.svc.Preview(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		httpx.Error(w, err, "That crew link does not work.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a.projectCrew(c))
}

func (a *API) join(w http.ResponseWriter, r *http.Request) {
	c, err := a.svc.Join(r.Context(), auth.MustUser(r.Context()).ID, chi.URLParam(r, "code"))
	if err != nil {
		httpx.Error(w, err, "You could not join that crew.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a.projectCrew(c))
}

func (a *API) board(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "crewID")
	if !ok {
		return
	}
	b, err := a.svc.Board(r.Context(), id, auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "The crew could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a.projectBoard(b))
}

// remove takes a member out; taking yourself out is leaving.
func (a *API) remove(w http.ResponseWriter, r *http.Request) {
	crewID, ok := pathID(w, r, "crewID")
	if !ok {
		return
	}
	memberID, ok := pathID(w, r, "userID")
	if !ok {
		return
	}
	me := auth.MustUser(r.Context()).ID
	var err error
	if memberID == me {
		err = a.svc.Leave(r.Context(), crewID, me)
	} else {
		err = a.svc.Remove(r.Context(), crewID, me, memberID)
	}
	if err != nil {
		httpx.Error(w, err, "That could not be done.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) setChallenge(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "crewID")
	if !ok {
		return
	}
	var req ChallengeView
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.svc.SetChallenge(r.Context(), id, auth.MustUser(r.Context()).ID, &Challenge{Kind: req.Kind, Target: req.Target}); err != nil {
		httpx.Error(w, err, "The challenge could not be set.")
		return
	}
	a.board(w, r)
}

func (a *API) clearChallenge(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "crewID")
	if !ok {
		return
	}
	if err := a.svc.SetChallenge(r.Context(), id, auth.MustUser(r.Context()).ID, nil); err != nil {
		httpx.Error(w, err, "The challenge could not be cleared.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) projectCrew(c Crew) CrewView {
	return CrewView{ID: c.ID, Name: c.Name, MemberCount: c.Members, JoinURL: JoinURL(a.siteURL, c.Code)}
}

func (a *API) projectBoard(b Board) BoardView {
	out := BoardView{CrewView: a.projectCrew(b.Crew), Members: make([]MemberView, 0, len(b.Members)), WeekStart: b.WeekStart, IsOwner: b.IsOwner}
	if b.Challenge != nil {
		out.Challenge = &ChallengeView{Kind: b.Challenge.Kind, Target: b.Challenge.Target}
	}
	for _, m := range b.Members {
		out.Members = append(out.Members, MemberView{
			ID: m.ID, DisplayName: m.DisplayName, Handle: m.Handle, CheckedIn: m.CheckedIn, WorkedOut: m.WorkedOut,
			Streak: m.Streak, WeekProgress: m.WeekProgress, Owner: m.Owner, Me: m.Me,
		})
	}
	return out
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
	if err := httpx.ReadJSON(w, r, dst, httpx.ReadOptions{MaxBytes: 4 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return false
	}
	return true
}
