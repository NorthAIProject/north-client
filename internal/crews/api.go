package crews

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/crews/live"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is crews for native clients. Mount behind auth.RequireBearer.
type API struct {
	svc       *Service
	siteURL   string
	chat      *Chat
	hub       *live.Hub
	heartbeat time.Duration
}

func NewAPI(svc *Service, siteURL string) *API {
	return &API{svc: svc, siteURL: strings.TrimRight(siteURL, "/"), heartbeat: heartbeat}
}

// WithLive gives the API the chat and the hub its streams read from.
func (a *API) WithLive(chat *Chat, hub *live.Hub) *API {
	a.chat, a.hub = chat, hub
	return a
}

// WithHeartbeat changes how often a quiet stream pings, for tests.
func (a *API) WithHeartbeat(d time.Duration) *API { a.heartbeat = d; return a }

func (a *API) Routes(r chi.Router) {
	r.Get("/crews/{crewID}/live", a.live)
	a.chatRoutes(r)
	r.Get("/crews", a.list)
	r.Post("/crews", a.create)
	r.Get("/crews/join/{code}", a.preview)
	r.Post("/crews/join/{code}", a.join)
	r.Get("/crews/{crewID}", a.board)
	r.Delete("/crews/{crewID}/members/{userID}", a.remove)
	r.Put("/crews/{crewID}/members/{userID}/role", a.setRole)
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
	// Unread and Mentions are filled on the crew list only.
	Unread   int `json:"unread,omitempty"`
	Mentions int `json:"mentions,omitempty"`
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
	// Role is owner, admin or member; clients treat anything else as member.
	Role string `json:"role"`
}

type BoardView struct {
	CrewView
	Challenge *ChallengeView `json:"challenge,omitempty"`
	Members   []MemberView   `json:"members"`
	WeekStart time.Time      `json:"weekStart"`
	IsOwner   bool           `json:"isOwner"`
	MyRole    string         `json:"myRole"`
}

type RoleRequest struct {
	Role string `json:"role"`
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
	unread, err := a.unreadByCrew(r)
	if err != nil {
		httpx.Error(w, err, "Crews could not be loaded.")
		return
	}
	out := CrewList{Crews: make([]CrewView, 0, len(list))}
	for _, c := range list {
		v := a.projectCrew(c)
		v.Unread, v.Mentions = unread[c.ID].Unread, unread[c.ID].Mentions
		out.Crews = append(out.Crews, v)
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

// setRole makes a member an admin or a member again; owner only.
func (a *API) setRole(w http.ResponseWriter, r *http.Request) {
	crewID, ok := pathID(w, r, "crewID")
	if !ok {
		return
	}
	memberID, ok := pathID(w, r, "userID")
	if !ok {
		return
	}
	var req RoleRequest
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.svc.SetRole(r.Context(), crewID, auth.MustUser(r.Context()).ID, memberID, Role(req.Role)); err != nil {
		httpx.Error(w, err, "The role could not be changed.")
		return
	}
	a.board(w, r)
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
	out := BoardView{CrewView: a.projectCrew(b.Crew), Members: make([]MemberView, 0, len(b.Members)), WeekStart: b.WeekStart, IsOwner: b.IsOwner, MyRole: string(b.MyRole)}
	if b.Challenge != nil {
		out.Challenge = &ChallengeView{Kind: b.Challenge.Kind, Target: b.Challenge.Target}
	}
	for _, m := range b.Members {
		out.Members = append(out.Members, MemberView{
			ID: m.ID, DisplayName: m.DisplayName, Handle: m.Handle, CheckedIn: m.CheckedIn, WorkedOut: m.WorkedOut,
			Streak: m.Streak, WeekProgress: m.WeekProgress, Owner: m.Owner, Me: m.Me, Role: string(m.Role),
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

// live streams the crew's events to a member, as JSON.
func (a *API) live(w http.ResponseWriter, r *http.Request) {
	crewID, ok := pathID(w, r, "crewID")
	if !ok {
		return
	}
	viewer := auth.MustUser(r.Context()).ID
	if _, _, err := a.chat.member(r.Context(), crewID, viewer); err != nil {
		httpx.Error(w, err, "The crew could not be loaded.")
		return
	}
	serveLive(w, r, a.hub, crewID, a.chat.viewerGate(viewer, apiFrame), a.heartbeat)
}
