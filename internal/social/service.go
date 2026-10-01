package social

import (
	"context"
	"crypto/rand"
	"regexp"
	"strings"

	"github.com/google/uuid"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Inbox tells somebody that something social happened to them: a follow
// request, a friend arriving from their link. nudges.Service satisfies it.
type Inbox interface {
	NoteWithPush(ctx context.Context, userID uuid.UUID, kind, dedupe, title, body, href string) error
}

// Funnel counts invites turning into accounts. analytics.Funnel satisfies it.
type Funnel interface {
	InviteRedeemed(ctx context.Context, inviteeID uuid.UUID, channel string)
}

// Nudge kinds this package raises. Plain strings so internal/nudges need not
// import this package; it treats kinds it does not know as allowed.
const (
	KindFollowRequest = "follow_request"
	KindInviteJoined  = "invite_joined"
)

type Service struct {
	repo   *Repository
	inbox  Inbox
	funnel Funnel
}

func NewService(repo *Repository) *Service { return &Service{repo: repo} }

func (s *Service) WithInbox(in Inbox) *Service { s.inbox = in; return s }

func (s *Service) WithFunnel(f Funnel) *Service { s.funnel = f; return s }

var handlePattern = regexp.MustCompile(`^[a-z0-9_]{3,20}$`)

// reservedHandles are words a stranger could use to look official.
var reservedHandles = map[string]bool{
	"admin": true, "administrator": true, "api": true, "app": true, "coach": true,
	"help": true, "khepri": true, "north": true, "official": true, "root": true,
	"security": true, "staff": true, "support": true, "system": true, "team": true,
}

// NormalizeHandle lower-cases and trims, dropping a leading @ somebody typed
// out of habit.
func NormalizeHandle(raw string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(raw), "@"))
}

// SetHandle claims a handle, or clears it when raw is empty. Clearing makes
// the account impossible to look up; existing follows are kept.
func (s *Service) SetHandle(ctx context.Context, userID uuid.UUID, raw string) (string, error) {
	handle := NormalizeHandle(raw)
	if handle == "" {
		return "", s.repo.SetHandle(ctx, userID, nil)
	}
	switch {
	case !handlePattern.MatchString(handle):
		return "", apperr.FieldErrors{}.Add("handle", "Use 3 to 20 letters, numbers or underscores.")
	case reservedHandles[handle]:
		return "", apperr.FieldErrors{}.Add("handle", "That handle is reserved. Try another.")
	}
	if err := s.repo.SetHandle(ctx, userID, &handle); err != nil {
		if err == errHandleTaken {
			return "", apperr.FieldErrors{}.Add("handle", "Somebody already has that handle.")
		}
		return "", err
	}
	return handle, nil
}

func validChannel(channel string) bool {
	switch channel {
	case ChannelLink, ChannelMessages, ChannelX, ChannelFacebook, ChannelContacts:
		return true
	}
	return false
}

// InviteFor returns the person's code for a channel, making it on first use.
// One code per channel keeps a link that was posted once working for good.
func (s *Service) InviteFor(ctx context.Context, userID uuid.UUID, channel string) (Invite, error) {
	if channel == "" {
		channel = ChannelLink
	}
	if !validChannel(channel) {
		return Invite{}, apperr.FieldErrors{}.Add("channel", "Unknown invite channel.")
	}
	return s.repo.InviteFor(ctx, userID, channel, newCode())
}

// PreviewInvite is the signed-out view of a code. An unknown code is not
// found, never an error page with a reason: codes are not to be guessed at.
func (s *Service) PreviewInvite(ctx context.Context, code string) (InvitePreview, error) {
	code = strings.ToLower(strings.TrimSpace(code))
	if !validCode(code) {
		return InvitePreview{}, apperr.ErrNotFound
	}
	return s.repo.InviteByCode(ctx, code)
}

// Redeem connects the account to whoever's link it opened: a new account, or
// a friend who was already here. connected is true when they were not
// already following each other. Only an account's first invite is counted
// as the one that brought them in.
//
// Safe to call more than once and with a code that no longer exists: an
// invite that cannot be honoured is dropped quietly rather than failing
// somebody's signup.
func (s *Service) Redeem(ctx context.Context, inviteeID uuid.UUID, code string) (bool, error) {
	invite, err := s.PreviewInvite(ctx, code)
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	if invite.Inviter.ID == inviteeID {
		return false, nil
	}
	blocked, err := s.repo.Blocked(ctx, inviteeID, invite.Inviter.ID)
	if err != nil || blocked {
		return false, err
	}
	result, err := s.repo.Redeem(ctx, inviteeID, invite.Code, invite.Inviter.ID)
	if err != nil {
		return false, err
	}
	if result.Attributed && s.funnel != nil {
		s.funnel.InviteRedeemed(ctx, inviteeID, invite.Channel)
	}
	if result.Connected && s.inbox != nil {
		if joined, err := s.repo.PersonByID(ctx, inviteeID); err == nil {
			title := joined.DisplayName + " joined from your invite"
			if !result.Attributed {
				title = joined.DisplayName + " accepted your invite"
			}
			_ = s.inbox.NoteWithPush(ctx, invite.Inviter.ID, KindInviteJoined, inviteeID.String(),
				title, "You follow each other now.", "/app/friends")
		}
	}
	return result.Connected, nil
}

// Follow asks to follow the person with this handle. It waits for their yes.
// A blocked or unknown handle is not found either way, so a block cannot be
// detected by trying to follow.
func (s *Service) Follow(ctx context.Context, followerID uuid.UUID, handle string) (Connection, error) {
	target, err := s.repo.PersonByHandle(ctx, NormalizeHandle(handle))
	if err != nil {
		return Connection{}, err
	}
	if target.ID == followerID {
		return Connection{}, apperr.FieldErrors{}.Add("handle", "That is you.")
	}
	blocked, err := s.repo.Blocked(ctx, followerID, target.ID)
	if err != nil {
		return Connection{}, err
	}
	if blocked {
		return Connection{}, apperr.ErrNotFound
	}
	before, err := s.repo.FollowStatus(ctx, followerID, target.ID)
	if err != nil {
		return Connection{}, err
	}
	status, err := s.repo.Follow(ctx, followerID, target.ID, StatusPending)
	if err != nil {
		return Connection{}, err
	}
	if before == "" && status == StatusPending && s.inbox != nil {
		if me, err := s.repo.PersonByID(ctx, followerID); err == nil {
			_ = s.inbox.NoteWithPush(ctx, target.ID, KindFollowRequest, followerID.String(),
				me.DisplayName+" wants to follow you", "Say yes or no in Friends.", "/app/friends")
		}
	}
	return Connection{Person: target, Status: status}, nil
}

// Accept says yes to a pending request from followerID.
func (s *Service) Accept(ctx context.Context, followeeID, followerID uuid.UUID) error {
	ok, err := s.repo.Accept(ctx, followerID, followeeID)
	if err != nil {
		return err
	}
	if !ok {
		return apperr.ErrNotFound
	}
	return nil
}

// RemoveFollower declines a request or removes an accepted follower. They are
// not told, and may ask again; a block is the way to stop that.
func (s *Service) RemoveFollower(ctx context.Context, followeeID, followerID uuid.UUID) error {
	return s.repo.Unfollow(ctx, followerID, followeeID)
}

// Unfollow stops following, or withdraws a pending request.
func (s *Service) Unfollow(ctx context.Context, followerID, followeeID uuid.UUID) error {
	return s.repo.Unfollow(ctx, followerID, followeeID)
}

func (s *Service) Block(ctx context.Context, blockerID, blockedID uuid.UUID) error {
	if blockerID == blockedID {
		return apperr.FieldErrors{}.Add("user", "You cannot block yourself.")
	}
	if _, err := s.repo.PersonByID(ctx, blockedID); err != nil {
		return err
	}
	return s.repo.Block(ctx, blockerID, blockedID)
}

func (s *Service) Unblock(ctx context.Context, blockerID, blockedID uuid.UUID) error {
	return s.repo.Unblock(ctx, blockerID, blockedID)
}

// Overview is everything the Friends page shows.
func (s *Service) Overview(ctx context.Context, userID uuid.UUID) (Overview, error) {
	var out Overview
	var err error
	if out.Handle, err = s.repo.Handle(ctx, userID); err != nil {
		return Overview{}, err
	}
	if out.Invite, err = s.InviteFor(ctx, userID, ChannelLink); err != nil {
		return Overview{}, err
	}
	if out.Joined, err = s.repo.Joined(ctx, userID); err != nil {
		return Overview{}, err
	}
	followers, err := s.repo.Followers(ctx, userID)
	if err != nil {
		return Overview{}, err
	}
	for _, c := range followers {
		if c.Status == StatusPending {
			out.Requests = append(out.Requests, c)
		} else {
			out.Followers = append(out.Followers, c)
		}
	}
	if out.Following, err = s.repo.Following(ctx, userID); err != nil {
		return Overview{}, err
	}
	if out.Blocked, err = s.repo.BlockedList(ctx, userID); err != nil {
		return Overview{}, err
	}
	return out, nil
}

// Profile is /u/<handle> as viewerID sees it. Blocked either way is not
// found, like a handle nobody holds.
func (s *Service) Profile(ctx context.Context, viewerID uuid.UUID, handle string) (Profile, error) {
	person, err := s.repo.PersonByHandle(ctx, NormalizeHandle(handle))
	if err != nil {
		return Profile{}, err
	}
	out := Profile{Person: person}
	if person.ID == viewerID {
		out.Relationship.Self = true
	} else {
		blocked, blockErr := s.repo.Blocked(ctx, viewerID, person.ID)
		if blockErr != nil {
			return Profile{}, blockErr
		}
		if blocked {
			return Profile{}, apperr.ErrNotFound
		}
		if out.Relationship.Following, err = s.repo.FollowStatus(ctx, viewerID, person.ID); err != nil {
			return Profile{}, err
		}
		theirs, theirErr := s.repo.FollowStatus(ctx, person.ID, viewerID)
		if theirErr != nil {
			return Profile{}, theirErr
		}
		out.Relationship.FollowsYou = theirs == StatusAccepted
	}
	if out.Followers, out.Following, err = s.repo.Counts(ctx, person.ID); err != nil {
		return Profile{}, err
	}
	return out, nil
}

// Invite codes: ten characters of lower-case letters and digits, without the
// ones that read alike (0/o, 1/l/i), so a code read aloud survives.
const codeAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

func newCode() string {
	// Bytes at or above the largest multiple of the alphabet's length are
	// skipped, so every character is equally likely.
	limit := byte(256 - 256%len(codeAlphabet))
	out := make([]byte, 0, 10)
	buf := make([]byte, 16)
	for len(out) < 10 {
		if _, err := rand.Read(buf); err != nil {
			panic(err) // crypto/rand does not fail on supported platforms
		}
		for _, b := range buf {
			if b < limit && len(out) < 10 {
				out = append(out, codeAlphabet[int(b)%len(codeAlphabet)])
			}
		}
	}
	return string(out)
}

func validCode(code string) bool {
	if len(code) != 10 {
		return false
	}
	for _, c := range code {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyz0123456789", c) {
			return false
		}
	}
	return true
}
