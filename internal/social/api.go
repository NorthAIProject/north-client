package social

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v5"

	"github.com/NorthAIProject/north-client/internal/auth"
	"github.com/NorthAIProject/north-client/internal/shared/echox"
	"github.com/NorthAIProject/north-client/internal/shared/httpx"
)

// API is the Friends page for native clients: handle, invite link, follows,
// blocks, and other people's public profiles.
type API struct {
	svc     *Service
	siteURL string
}

// NewAPI builds the routes. siteURL makes invite codes into links.
func NewAPI(svc *Service, siteURL string) *API {
	return &API{svc: svc, siteURL: strings.TrimRight(siteURL, "/")}
}

// RegisterPublic registers what needs no bearer token: who sent this link,
// and Facebook's return from a connection begun in the app.
func (a *API) RegisterPublic(g *echo.Group) {
	g.GET("/invites/:code", a.preview)
	g.GET("/social/facebook/callback", a.facebookCallback)
}

// Register registers the routes that go behind auth.Bearer.
func (a *API) Register(g *echo.Group) {
	g.GET("/social", a.overview)
	g.PUT("/social/handle", a.setHandle)
	g.GET("/invites", a.invite)
	g.POST("/invites/:code/redeem", a.redeem)
	g.POST("/follows", a.follow)
	g.DELETE("/follows/:userID", a.unfollow)
	g.POST("/followers/:userID/accept", a.accept)
	g.DELETE("/followers/:userID", a.removeFollower)
	g.POST("/friends/match", a.matchContacts)
	g.POST("/blocks/:userID", a.block)
	g.DELETE("/blocks/:userID", a.unblock)
	g.GET("/social/phone", a.phone)
	g.DELETE("/social/phone", a.removePhone)
	g.POST("/social/phone/verification", a.startPhone)
	g.DELETE("/social/phone/verification", a.cancelPhone)
	g.POST("/social/phone/verification/check", a.checkPhone)
	g.GET("/social/facebook", a.facebookFriends)
	g.DELETE("/social/facebook", a.disconnectFacebook)
	g.POST("/social/facebook/connect", a.connectFacebook)
	a.registerWeb(g)
}

type PersonView struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"displayName"`
	// Handle is empty for somebody who never chose one.
	Handle string `json:"handle"`
}

type ConnectionView struct {
	PersonView
	// Status is accepted; pending is legacy and no longer produced.
	Status string    `json:"status"`
	Since  time.Time `json:"since"`
}

type InviteView struct {
	Code    string `json:"code"`
	Channel string `json:"channel"`
	URL     string `json:"url"`
}

type SocialOverview struct {
	Handle    string           `json:"handle"`
	Invite    InviteView       `json:"invite"`
	Joined    int              `json:"joined"`
	Requests  []ConnectionView `json:"requests"` // always empty: follows are instant; kept so shipped iOS still decodes
	Followers []ConnectionView `json:"followers"`
	Following []ConnectionView `json:"following"`
	Blocked   []PersonView     `json:"blocked"`
}

type InvitePreviewView struct {
	Code    string     `json:"code"`
	Inviter PersonView `json:"inviter"`
}

type RelationshipView struct {
	// Following is "" or accepted (pending is legacy).
	Following  string `json:"following"`
	FollowsYou bool   `json:"followsYou"`
	Self       bool   `json:"self"`
}

type ProfileView struct {
	PersonView
	Followers    int              `json:"followers"`
	Following    int              `json:"following"`
	Relationship RelationshipView `json:"relationship"`
}

type HandleRequest struct {
	// Handle is 3-20 letters, digits or underscores; empty clears it.
	Handle string `json:"handle"`
}

type HandleView struct {
	Handle string `json:"handle"`
}

type FollowRequest struct {
	Handle string `json:"handle"`
}

type RedeemView struct {
	// Redeemed is false when this account was already invited, or the code no
	// longer leads anywhere; neither is an error.
	Redeemed bool `json:"redeemed"`
}

func (a *API) overview(c *echo.Context) error {
	ctx := c.Request().Context()
	o, err := a.svc.Overview(ctx, auth.MustUser(ctx).ID)
	if err != nil {
		return httpx.Fail(err, "Friends could not be loaded.")
	}
	return c.JSON(http.StatusOK, a.projectOverview(o))
}

func (a *API) setHandle(c *echo.Context) error {
	var req HandleRequest
	if err := readJSON(c, &req); err != nil {
		return err
	}
	ctx := c.Request().Context()
	handle, err := a.svc.SetHandle(ctx, auth.MustUser(ctx).ID, req.Handle)
	if err != nil {
		return httpx.Fail(err, "That handle could not be saved.")
	}
	return c.JSON(http.StatusOK, HandleView{Handle: handle})
}

func (a *API) invite(c *echo.Context) error {
	ctx := c.Request().Context()
	inv, err := a.svc.InviteFor(ctx, auth.MustUser(ctx).ID, c.QueryParam("channel"))
	if err != nil {
		return httpx.Fail(err, "Your invite link could not be made.")
	}
	return c.JSON(http.StatusOK, a.projectInvite(inv))
}

func (a *API) preview(c *echo.Context) error {
	p, err := a.svc.PreviewInvite(c.Request().Context(), c.Param("code"))
	if err != nil {
		return httpx.Fail(err, "That invite link does not work.")
	}
	return c.JSON(http.StatusOK, InvitePreviewView{Code: p.Code, Inviter: projectPerson(p.Inviter)})
}

func (a *API) redeem(c *echo.Context) error {
	ctx := c.Request().Context()
	ok, err := a.svc.Redeem(ctx, auth.MustUser(ctx).ID, c.Param("code"))
	if err != nil {
		return httpx.Fail(err, "That invite could not be used.")
	}
	return c.JSON(http.StatusOK, RedeemView{Redeemed: ok})
}

// ProjectProfile is the profile as the API sends it. The route lives in
// profiles, which adds what the person shares; it reuses this projection.
func ProjectProfile(p Profile) ProfileView {
	return ProfileView{
		PersonView: projectPerson(p.Person), Followers: p.Followers, Following: p.Following,
		Relationship: RelationshipView{Following: p.Relationship.Following, FollowsYou: p.Relationship.FollowsYou, Self: p.Relationship.Self},
	}
}

func (a *API) follow(c *echo.Context) error {
	var req FollowRequest
	if err := readJSON(c, &req); err != nil {
		return err
	}
	ctx := c.Request().Context()
	conn, err := a.svc.Follow(ctx, auth.MustUser(ctx).ID, req.Handle)
	if err != nil {
		return httpx.Fail(err, "Nobody has that handle.")
	}
	return c.JSON(http.StatusOK, projectConnection(conn))
}

func (a *API) unfollow(c *echo.Context) error {
	return a.onPerson(c, "You could not unfollow them.", a.svc.Unfollow)
}

func (a *API) accept(c *echo.Context) error {
	return a.onPerson(c, "That request could not be accepted.", a.svc.Accept)
}

func (a *API) removeFollower(c *echo.Context) error {
	return a.onPerson(c, "They could not be removed.", a.svc.RemoveFollower)
}

func (a *API) block(c *echo.Context) error {
	return a.onPerson(c, "They could not be blocked.", a.svc.Block)
}

func (a *API) unblock(c *echo.Context) error {
	return a.onPerson(c, "They could not be unblocked.", a.svc.Unblock)
}

// onPerson runs an action between the signed-in account and :userID, and
// answers 204.
func (a *API) onPerson(c *echo.Context, failure string,
	act func(ctx context.Context, me, them uuid.UUID) error,
) error {
	them, err := echox.UUIDParam(c, "userID")
	if err != nil {
		return err
	}
	ctx := c.Request().Context()
	if err := act(ctx, auth.MustUser(ctx).ID, them); err != nil {
		return httpx.Fail(err, failure)
	}
	return c.NoContent(http.StatusNoContent)
}

func (a *API) projectOverview(o Overview) SocialOverview {
	out := SocialOverview{
		Handle: o.Handle, Invite: a.projectInvite(o.Invite), Joined: o.Joined,
		Requests: projectConnections(o.Requests), Followers: projectConnections(o.Followers),
		Following: projectConnections(o.Following), Blocked: []PersonView{},
	}
	for _, p := range o.Blocked {
		out.Blocked = append(out.Blocked, projectPerson(p))
	}
	return out
}

func (a *API) projectInvite(inv Invite) InviteView {
	return InviteView{Code: inv.Code, Channel: inv.Channel, URL: InviteURL(a.siteURL, inv.Code)}
}

// InviteURL is the link somebody shares.
func InviteURL(siteURL, code string) string {
	return strings.TrimRight(siteURL, "/") + "/i/" + code
}

func projectPerson(p Person) PersonView {
	return PersonView{ID: p.ID, DisplayName: p.DisplayName, Handle: p.Handle}
}

func projectConnection(c Connection) ConnectionView {
	return ConnectionView{PersonView: projectPerson(c.Person), Status: c.Status, Since: c.Since}
}

func projectConnections(cs []Connection) []ConnectionView {
	out := make([]ConnectionView, 0, len(cs))
	for _, c := range cs {
		out = append(out, projectConnection(c))
	}
	return out
}

func readJSON(c *echo.Context, dst any) error {
	return echox.BindJSON(c, dst, httpx.ReadOptions{MaxBytes: 16 << 10}, "The request body could not be read.")
}

type MatchRequest struct {
	// Hashes are SHA-256 hex digests of lower-cased email addresses.
	Hashes []string `json:"hashes"`
	// PhoneHashes are SHA-256 hex digests of E.164 phone numbers
	// ("+351912345678"). Hashes and PhoneHashes share the 2000 cap.
	PhoneHashes []string `json:"phoneHashes"`
}

// MatchedPerson is somebody found in your contacts.
type MatchedPerson struct {
	PersonView
	// Following is "" or accepted (pending is legacy): how you follow them already.
	Following string `json:"following"`
}

type MatchView struct {
	People []MatchedPerson `json:"people"`
}

func (a *API) matchContacts(c *echo.Context) error {
	var req MatchRequest
	if err := echox.BindJSON(c, &req, httpx.ReadOptions{MaxBytes: 256 << 10}, "The request body could not be read."); err != nil {
		return err
	}
	ctx := c.Request().Context()
	found, err := a.svc.MatchContacts(ctx, auth.MustUser(ctx).ID, req.Hashes, req.PhoneHashes)
	if err != nil {
		return httpx.Fail(err, "Your contacts could not be checked.")
	}
	return c.JSON(http.StatusOK, projectMatches(found))
}

func projectMatches(found []Connection) MatchView {
	out := MatchView{People: make([]MatchedPerson, 0, len(found))}
	for _, c := range found {
		out.People = append(out.People, MatchedPerson{PersonView: projectPerson(c.Person), Following: c.Status})
	}
	return out
}
