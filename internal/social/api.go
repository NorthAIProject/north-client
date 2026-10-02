package social

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/auth"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
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

// PublicRoutes are what needs no bearer token: who sent this link, and
// Facebook's return from a connection begun in the app.
func (a *API) PublicRoutes(r chi.Router) {
	r.Get("/invites/{code}", a.preview)
	r.Get("/social/facebook/callback", a.facebookCallback)
}

// Routes go behind auth.RequireBearer.
func (a *API) Routes(r chi.Router) {
	r.Get("/social", a.overview)
	r.Put("/social/handle", a.setHandle)
	r.Get("/invites", a.invite)
	r.Post("/invites/{code}/redeem", a.redeem)
	r.Get("/profiles/{handle}", a.profile)
	r.Post("/follows", a.follow)
	r.Delete("/follows/{userID}", a.unfollow)
	r.Post("/followers/{userID}/accept", a.accept)
	r.Delete("/followers/{userID}", a.removeFollower)
	r.Post("/friends/match", a.matchContacts)
	r.Post("/blocks/{userID}", a.block)
	r.Delete("/blocks/{userID}", a.unblock)
	r.Get("/social/phone", a.phone)
	r.Delete("/social/phone", a.removePhone)
	r.Post("/social/phone/verification", a.startPhone)
	r.Delete("/social/phone/verification", a.cancelPhone)
	r.Post("/social/phone/verification/check", a.checkPhone)
	r.Get("/social/facebook", a.facebookFriends)
	r.Delete("/social/facebook", a.disconnectFacebook)
	r.Post("/social/facebook/connect", a.connectFacebook)
}

type PersonView struct {
	ID          uuid.UUID `json:"id"`
	DisplayName string    `json:"displayName"`
	// Handle is empty for somebody who never chose one.
	Handle string `json:"handle"`
}

type ConnectionView struct {
	PersonView
	// Status is pending or accepted.
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
	Requests  []ConnectionView `json:"requests"`
	Followers []ConnectionView `json:"followers"`
	Following []ConnectionView `json:"following"`
	Blocked   []PersonView     `json:"blocked"`
}

type InvitePreviewView struct {
	Code    string     `json:"code"`
	Inviter PersonView `json:"inviter"`
}

type RelationshipView struct {
	// Following is "", pending or accepted.
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

func (a *API) overview(w http.ResponseWriter, r *http.Request) {
	o, err := a.svc.Overview(r.Context(), auth.MustUser(r.Context()).ID)
	if err != nil {
		httpx.Error(w, err, "Friends could not be loaded.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a.projectOverview(o))
}

func (a *API) setHandle(w http.ResponseWriter, r *http.Request) {
	var req HandleRequest
	if !readJSON(w, r, &req) {
		return
	}
	handle, err := a.svc.SetHandle(r.Context(), auth.MustUser(r.Context()).ID, req.Handle)
	if err != nil {
		httpx.Error(w, err, "That handle could not be saved.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, HandleView{Handle: handle})
}

func (a *API) invite(w http.ResponseWriter, r *http.Request) {
	inv, err := a.svc.InviteFor(r.Context(), auth.MustUser(r.Context()).ID, r.URL.Query().Get("channel"))
	if err != nil {
		httpx.Error(w, err, "Your invite link could not be made.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a.projectInvite(inv))
}

func (a *API) preview(w http.ResponseWriter, r *http.Request) {
	p, err := a.svc.PreviewInvite(r.Context(), chi.URLParam(r, "code"))
	if err != nil {
		httpx.Error(w, err, "That invite link does not work.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, InvitePreviewView{Code: p.Code, Inviter: projectPerson(p.Inviter)})
}

func (a *API) redeem(w http.ResponseWriter, r *http.Request) {
	ok, err := a.svc.Redeem(r.Context(), auth.MustUser(r.Context()).ID, chi.URLParam(r, "code"))
	if err != nil {
		httpx.Error(w, err, "That invite could not be used.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, RedeemView{Redeemed: ok})
}

func (a *API) profile(w http.ResponseWriter, r *http.Request) {
	p, err := a.svc.Profile(r.Context(), auth.MustUser(r.Context()).ID, chi.URLParam(r, "handle"))
	if err != nil {
		httpx.Error(w, err, "Nobody has that handle.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ProfileView{
		PersonView: projectPerson(p.Person), Followers: p.Followers, Following: p.Following,
		Relationship: RelationshipView{Following: p.Relationship.Following, FollowsYou: p.Relationship.FollowsYou, Self: p.Relationship.Self},
	})
}

func (a *API) follow(w http.ResponseWriter, r *http.Request) {
	var req FollowRequest
	if !readJSON(w, r, &req) {
		return
	}
	c, err := a.svc.Follow(r.Context(), auth.MustUser(r.Context()).ID, req.Handle)
	if err != nil {
		httpx.Error(w, err, "Nobody has that handle.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectConnection(c))
}

func (a *API) unfollow(w http.ResponseWriter, r *http.Request) {
	a.onPerson(w, r, "You could not unfollow them.", a.svc.Unfollow)
}

func (a *API) accept(w http.ResponseWriter, r *http.Request) {
	a.onPerson(w, r, "That request could not be accepted.", a.svc.Accept)
}

func (a *API) removeFollower(w http.ResponseWriter, r *http.Request) {
	a.onPerson(w, r, "They could not be removed.", a.svc.RemoveFollower)
}

func (a *API) block(w http.ResponseWriter, r *http.Request) {
	a.onPerson(w, r, "They could not be blocked.", a.svc.Block)
}

func (a *API) unblock(w http.ResponseWriter, r *http.Request) {
	a.onPerson(w, r, "They could not be unblocked.", a.svc.Unblock)
}

// onPerson runs an action between the signed-in account and {userID}, and
// answers 204.
func (a *API) onPerson(w http.ResponseWriter, r *http.Request, failure string,
	act func(ctx context.Context, me, them uuid.UUID) error,
) {
	them, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		httpx.Error(w, apperr.ErrNotFound, "Not found.")
		return
	}
	if err := act(r.Context(), auth.MustUser(r.Context()).ID, them); err != nil {
		httpx.Error(w, err, failure)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := httpx.ReadJSON(w, r, dst, httpx.ReadOptions{MaxBytes: 16 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return false
	}
	return true
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
	// Following is "", pending or accepted: how you follow them already.
	Following string `json:"following"`
}

type MatchView struct {
	People []MatchedPerson `json:"people"`
}

func (a *API) matchContacts(w http.ResponseWriter, r *http.Request) {
	var req MatchRequest
	if err := httpx.ReadJSON(w, r, &req, httpx.ReadOptions{MaxBytes: 256 << 10}); err != nil {
		httpx.Error(w, err, "The request body could not be read.")
		return
	}
	found, err := a.svc.MatchContacts(r.Context(), auth.MustUser(r.Context()).ID, req.Hashes, req.PhoneHashes)
	if err != nil {
		httpx.Error(w, err, "Your contacts could not be checked.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, projectMatches(found))
}

func projectMatches(found []Connection) MatchView {
	out := MatchView{People: make([]MatchedPerson, 0, len(found))}
	for _, c := range found {
		out.People = append(out.People, MatchedPerson{PersonView: projectPerson(c.Person), Following: c.Status})
	}
	return out
}
