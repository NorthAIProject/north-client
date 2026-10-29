package crews

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/crews/live"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is crews for native clients. Register behind auth.Bearer.
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

// RegisterPublic registers the routes that need no bearer: the crew behind a
// join link, which the signed-out landing page at /i/c/{code} shows (the Go
// page shows it too).
func (a *API) RegisterPublic(g *echo.Group) {
	g.GET("/crews/join/:code", a.preview)
}

func (a *API) Register(g *echo.Group) {
	g.GET("/crews/:crewID/live", a.live)
	a.registerChat(g)
	g.GET("/crews", a.list)
	g.POST("/crews", a.create)
	g.POST("/crews/join/:code", a.join)
	g.GET("/crews/:crewID", a.board)
	g.DELETE("/crews/:crewID/members/:userID", a.remove)
	g.PUT("/crews/:crewID/members/:userID/role", a.setRole)
	g.PUT("/crews/:crewID/challenge", a.setChallenge)
	g.DELETE("/crews/:crewID/challenge", a.clearChallenge)
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

func (a *API) list(c *echo.Context) error {
	ctx := c.Request().Context()
	list, err := a.svc.Mine(ctx, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "Crews could not be loaded.")
	}
	unread, err := a.unreadByCrew(ctx)
	if err != nil {
		return httpx.Fail(err, "Crews could not be loaded.")
	}
	out := CrewList{Crews: make([]CrewView, 0, len(list))}
	for _, cr := range list {
		v := a.projectCrew(cr)
		v.Unread, v.Mentions = unread[cr.ID].Unread, unread[cr.ID].Mentions
		out.Crews = append(out.Crews, v)
	}
	return c.JSON(http.StatusOK, out)
}

func (a *API) create(c *echo.Context) error {
	var req CreateRequest
	if err := readJSON(c, &req); err != nil {
		return err
	}
	ctx := c.Request().Context()
	cr, err := a.svc.Create(ctx, auth.MustUser(ctx).ID, req.Name)
	if err != nil {
		return httpx.Fail(err, "The crew could not be made.")
	}
	return c.JSON(http.StatusCreated, a.projectCrew(cr))
}

func (a *API) preview(c *echo.Context) error {
	cr, err := a.svc.Preview(c.Request().Context(), c.Param("code"))
	if err != nil {
		return httpx.Fail(err, "That crew link does not work.")
	}
	return c.JSON(http.StatusOK, a.projectCrew(cr))
}

func (a *API) join(c *echo.Context) error {
	ctx := c.Request().Context()
	cr, err := a.svc.Join(ctx, auth.MustUser(ctx).ID, c.Param("code"))
	if err != nil {
		return httpx.Fail(err, "You could not join that crew.")
	}
	return c.JSON(http.StatusOK, a.projectCrew(cr))
}

func (a *API) board(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "crewID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	b, err := a.svc.Board(ctx, id, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "The crew could not be loaded.")
	}
	return c.JSON(http.StatusOK, a.projectBoard(b))
}

// remove takes a member out; taking yourself out is leaving.
func (a *API) remove(c *echo.Context) error {
	crewID, memberID, err := twoIDs(c, "userID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	me := auth.MustUser(ctx).ID
	if memberID == me {
		err = a.svc.Leave(ctx, crewID, me)
	} else {
		err = a.svc.Remove(ctx, crewID, me, memberID)
	}
	if err != nil {
		return httpx.Fail(err, "That could not be done.")
	}
	return c.NoContent(http.StatusNoContent)
}

// setRole makes a member an admin or a member again; owner only.
func (a *API) setRole(c *echo.Context) error {
	crewID, memberID, err := twoIDs(c, "userID")
	if err != nil {
		return err
	}
	var req RoleRequest
	if err := readJSON(c, &req); err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err := a.svc.SetRole(ctx, crewID, auth.MustUser(ctx).ID, memberID, Role(req.Role)); err != nil {
		return httpx.Fail(err, "The role could not be changed.")
	}
	return a.board(c)
}

func (a *API) setChallenge(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "crewID")
	if err != nil {
		return err
	}
	var req ChallengeView
	if err := readJSON(c, &req); err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err := a.svc.SetChallenge(ctx, id, auth.MustUser(ctx).ID, &Challenge{Kind: req.Kind, Target: req.Target}); err != nil {
		return httpx.Fail(err, "The challenge could not be set.")
	}
	return a.board(c)
}

func (a *API) clearChallenge(c *echo.Context) error {
	id, err := echox.UUIDParam(c, "crewID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err := a.svc.SetChallenge(ctx, id, auth.MustUser(ctx).ID, nil); err != nil {
		return httpx.Fail(err, "The challenge could not be cleared.")
	}
	return c.NoContent(http.StatusNoContent)
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

func readJSON(c *echo.Context, dst any) error {
	return echox.BindJSON(c, dst, httpx.ReadOptions{MaxBytes: 4 << 10}, "The request body could not be read.")
}

// live streams the crew's events to a member, as JSON. Once the stream has
// started nothing is returned: serveLive writes and flushes through
// c.Response() until the client leaves.
func (a *API) live(c *echo.Context) error {
	crewID, err := echox.UUIDParam(c, "crewID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	viewer := auth.MustUser(ctx).ID
	if _, _, err := a.chat.member(ctx, crewID, viewer); err != nil {
		return httpx.Fail(err, "The crew could not be loaded.")
	}
	serveLive(c.Response(), c.Request(), a.hub, crewID, a.chat.viewerGate(viewer, apiFrame), a.heartbeat)
	return nil
}
