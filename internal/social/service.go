package social

import (
	"context"
	"crypto/rand"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/awards/award"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/social/phone"
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
	KindNewFollower  = "new_follower"
	KindInviteJoined = "invite_joined"
)

type Service struct {
	repo     *Repository
	inbox    Inbox
	funnel   Funnel
	verifier phone.Verifier
	facebook Facebook
	awards   Awards
}

// Awards checks a person's badges after a write. awards.Service satisfies it.
type Awards interface {
	Check(ctx context.Context, userID uuid.UUID, at time.Time, areas ...award.Area)
}

func (s *Service) WithAwards(a Awards) *Service { s.awards = a; return s }

func NewService(repo *Repository) *Service { return &Service{repo: repo} }

func (s *Service) WithInbox(in Inbox) *Service { s.inbox = in; return s }

func (s *Service) WithFunnel(f Funnel) *Service { s.funnel = f; return s }

var handlePattern = regexp.MustCompile(`^[a-z0-9_]{3,20}$`)

// reservedHandles are words a stranger could use to look official.
var reservedHandles = map[string]bool{
	"admin": true, "administrator": true, "api": true, "app": true, "coach": true,
	"help": true, "khepri": true, "north": true, "official": true, "root": true,
	"security": true, "staff": true, "support": true, "system": true, "team": true,
	// What a crew message uses to reach many people at once.
	"everyone": true, "here": true, "crew": true,
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
	// Both sides now follow each other: either may have just gained a first
	// follower.
	if result.Connected && s.awards != nil {
		now := time.Now()
		s.awards.Check(ctx, invite.Inviter.ID, now, award.Social)
		s.awards.Check(ctx, inviteeID, now, award.Social)
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

// Follow follows the person with this handle at once; there is no request to
// approve. They are told once, the first time. A blocked or unknown handle is
// not found either way, so a block cannot be detected by trying to follow.
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
	status, err := s.repo.Follow(ctx, followerID, target.ID, StatusAccepted)
	if err != nil {
		return Connection{}, err
	}
	if before == "" && s.inbox != nil {
		if me, err := s.repo.PersonByID(ctx, followerID); err == nil {
			_ = s.inbox.NoteWithPush(ctx, target.ID, KindNewFollower, followerID.String(),
				me.DisplayName+" followed you", "See who follows you in Friends.", "/app/friends")
		}
	}
	// The followee is the one who earns first_follower, and only for a
	// follow that was not already there.
	if before == "" && s.awards != nil {
		s.awards.Check(ctx, target.ID, time.Now(), award.Social)
	}
	return Connection{Person: target, Status: status}, nil
}

// Accept is kept for shipped clients that still show a stale request. Follows
// are accepted on creation, so an accepted follower is a harmless success and
// only a follower who does not exist is not found.
func (s *Service) Accept(ctx context.Context, followeeID, followerID uuid.UUID) error {
	status, err := s.repo.FollowStatus(ctx, followerID, followeeID)
	if err != nil {
		return err
	}
	switch status {
	case StatusAccepted:
		return nil
	case "":
		return apperr.ErrNotFound
	}
	ok, err := s.repo.Accept(ctx, followerID, followeeID)
	if err != nil {
		return err
	}
	if !ok {
		return apperr.ErrNotFound
	}
	return nil
}

// RemoveFollower removes a follower. They are not told, and may follow again;
// a block is the way to stop that.
func (s *Service) RemoveFollower(ctx context.Context, followeeID, followerID uuid.UUID) error {
	return s.repo.Unfollow(ctx, followerID, followeeID)
}

// Unfollow stops following.
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
	out.Followers = append(out.Followers, followers...)
	if out.Following, err = s.repo.Following(ctx, userID); err != nil {
		return Overview{}, err
	}
	if out.Blocked, err = s.repo.BlockedList(ctx, userID); err != nil {
		return Overview{}, err
	}
	return out, nil
}

// HandleOf is the person's own handle, empty when they have none.
func (s *Service) HandleOf(ctx context.Context, userID uuid.UUID) (string, error) {
	return s.repo.Handle(ctx, userID)
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

// MaxContactHashes bounds one match request, emails and phone numbers
// together: a big address book, not a list somebody scraped.
const MaxContactHashes = 2000

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// MatchContacts finds people already here among the viewer's contacts. The
// phone sends SHA-256 hashes of lower-cased emails and of E.164 phone
// numbers ("+351912345678"), never the addresses or numbers; the hashes are
// used for this query and not kept. A phone number only finds an account
// that verified it. Each result's Status is how the viewer follows them:
// "", pending or accepted.
func (s *Service) MatchContacts(ctx context.Context, viewerID uuid.UUID, emailHashes, phoneHashes []string) ([]Connection, error) {
	if len(emailHashes)+len(phoneHashes) > MaxContactHashes {
		return nil, apperr.FieldErrors{}.Add("hashes", "Send at most 2000 contacts at a time.")
	}
	emails, ok := cleanHashes(emailHashes)
	if !ok {
		return nil, apperr.FieldErrors{}.Add("hashes", "Each contact must be a SHA-256 hex digest.")
	}
	phones, ok := cleanHashes(phoneHashes)
	if !ok {
		return nil, apperr.FieldErrors{}.Add("phoneHashes", "Each phone number must be a SHA-256 hex digest.")
	}
	if len(emails)+len(phones) == 0 {
		return []Connection{}, nil
	}
	return s.repo.MatchContacts(ctx, viewerID, emails, phones)
}

// cleanHashes lower-cases and trims each hash, and reports false if any is
// not a SHA-256 hex digest.
func cleanHashes(hashes []string) ([]string, bool) {
	clean := make([]string, 0, len(hashes))
	for _, h := range hashes {
		h = strings.ToLower(strings.TrimSpace(h))
		if !sha256Hex.MatchString(h) {
			return nil, false
		}
		clean = append(clean, h)
	}
	return clean, true
}
