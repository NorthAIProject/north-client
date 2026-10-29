// Package crews keeps small groups that keep each other going: who checked
// in and trained today, streaks, and one weekly challenge. A crew of two is
// an accountability partner.
package crews

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/NorthAIProject/north-client/internal/crews/crew"
	"github.com/NorthAIProject/north-client/internal/crews/live"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/i18n"
	"github.com/NorthAIProject/north-client/internal/users"
)

type (
	Crew         = crew.Crew
	Challenge    = crew.Challenge
	Member       = crew.Member
	Board        = crew.Board
	Role         = crew.Role
	Channel      = crew.Channel
	Space        = crew.Space
	SpaceChannel = crew.SpaceChannel
	SpaceMember  = crew.SpaceMember
)

type Service struct {
	repo     Repository
	checkIns CheckInReader
	workouts WorkoutReader
	blocks   BlockChecker
	now      func() time.Time
}

// CheckInReader and WorkoutReader are narrowed to what the board needs and
// adapted in cmd from the check-ins and activity services, so this package
// does not depend on their types.
type CheckInReader interface {
	CheckedInOn(ctx context.Context, userID uuid.UUID, day time.Time) (bool, error)
	Streak(ctx context.Context, user users.User, now time.Time) (int, error)
	CountBetween(ctx context.Context, userID uuid.UUID, from, to time.Time) (int, error)
}

type WorkoutReader interface {
	CountBetween(ctx context.Context, userID uuid.UUID, from, to time.Time) (int, error)
}

// BlockChecker says whether either of two people has blocked the other.
// *social.PostgresRepository is one; it is narrowed here so crews does not
// depend on the social slice.
type BlockChecker interface {
	Blocked(ctx context.Context, a, b uuid.UUID) (bool, error)
}

// Repository is the persistence Service depends on: crews, their members,
// channels and challenges, and the live notice that someone left.
// NewRepository is what the process wires, and a test can hand Service
// anything that keeps crews.
type Repository interface {
	Notify(ctx context.Context, ev live.Event) error
	CreateCrew(ctx context.Context, ownerID uuid.UUID, name, code string) (Crew, error)
	CrewByCode(ctx context.Context, code string) (Crew, error)
	GetCrew(ctx context.Context, crewID uuid.UUID) (Crew, error)
	CountMembers(ctx context.Context, crewID uuid.UUID) (int, error)
	IsMember(ctx context.Context, crewID, userID uuid.UUID) (bool, error)
	AddMember(ctx context.Context, crewID, userID uuid.UUID) error
	Members(ctx context.Context, crewID uuid.UUID) ([]memberRecord, error)
	OldestOtherMember(ctx context.Context, crewID, userID uuid.UUID) (uuid.UUID, error)
	DeleteCrew(ctx context.Context, crewID uuid.UUID) error
	SetOwner(ctx context.Context, crewID, ownerID uuid.UUID) error
	RemoveMember(ctx context.Context, crewID, userID uuid.UUID) error
	SetMemberRole(ctx context.Context, crewID, userID uuid.UUID, role Role) error
	MemberRole(ctx context.Context, crewID, userID uuid.UUID) (Role, error)
	MemberSettings(ctx context.Context, crewID, userID uuid.UUID) (string, *time.Time, error)
	SetNotify(ctx context.Context, crewID, userID uuid.UUID, notify string, mutedUntil *time.Time) error
	ListMine(ctx context.Context, userID uuid.UUID) ([]Crew, error)
	CrewmatesOf(ctx context.Context, userID uuid.UUID) ([]crewmate, error)
	Challenge(ctx context.Context, crewID uuid.UUID) (*Challenge, error)
	ClearChallenge(ctx context.Context, crewID uuid.UUID) error
	SetChallenge(ctx context.Context, crewID uuid.UUID, kind string, target int) error
	ChallengesOf(ctx context.Context, userID uuid.UUID) ([]crewChallenge, error)
	ListChannels(ctx context.Context, crewID uuid.UUID) ([]Channel, error)
	ChannelInCrew(ctx context.Context, crewID, channelID uuid.UUID) (Channel, error)
	CountChannels(ctx context.Context, crewID uuid.UUID) (int, error)
	CreateChannel(ctx context.Context, crewID, createdBy uuid.UUID, name, topic string) (Channel, error)
	UpdateChannel(ctx context.Context, crewID, channelID uuid.UUID, name, topic string) (Channel, error)
	DeleteChannel(ctx context.Context, crewID, channelID uuid.UUID) error
	MessagesAuthoredBy(ctx context.Context, userID uuid.UUID) ([]crew.ExportedMessage, error)
	Unread(ctx context.Context, userID uuid.UUID) ([]Unread, error)
}

func NewService(repo Repository, checkIns CheckInReader, workouts WorkoutReader) *Service {
	return &Service{repo: repo, checkIns: checkIns, workouts: workouts, now: time.Now}
}

// WithBlocks keeps people out of crews whose owner they have blocked or been
// blocked by.
func (s *Service) WithBlocks(b BlockChecker) *Service { s.blocks = b; return s }

// WithClock fixes now, for tests.
func (s *Service) WithClock(now func() time.Time) *Service { s.now = now; return s }

func validName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 40 {
		return "", apperr.FieldErrors{}.Add("name", "Give the crew a name up to 40 characters.")
	}
	return name, nil
}

// Create makes a crew with its creator as owner and first member.
func (s *Service) Create(ctx context.Context, ownerID uuid.UUID, name string) (Crew, error) {
	name, err := validName(name)
	if err != nil {
		return Crew{}, err
	}
	c, err := s.repo.CreateCrew(ctx, ownerID, name, newCode())
	if err != nil {
		return Crew{}, err
	}
	c.Members = 1
	return c, nil
}

// Preview is the crew behind a join code, for the join page.
func (s *Service) Preview(ctx context.Context, code string) (Crew, error) {
	c, err := s.repo.CrewByCode(ctx, strings.ToLower(strings.TrimSpace(code)))
	if err != nil {
		return Crew{}, err
	}
	if c.Members, err = s.repo.CountMembers(ctx, c.ID); err != nil {
		return Crew{}, err
	}
	return c, nil
}

// Join adds the user to the crew behind code, if it has room. Joining twice
// is a no-op.
func (s *Service) Join(ctx context.Context, userID uuid.UUID, code string) (Crew, error) {
	c, err := s.Preview(ctx, code)
	if err != nil {
		return Crew{}, err
	}
	member, err := s.repo.IsMember(ctx, c.ID, userID)
	if err != nil {
		return Crew{}, err
	}
	if member {
		return c, nil
	}
	if s.blocks != nil {
		blocked, blockErr := s.blocks.Blocked(ctx, userID, c.OwnerID)
		if blockErr != nil {
			return Crew{}, blockErr
		}
		if blocked {
			// The same words whichever way the block points, so a refusal
			// never tells anybody they were blocked.
			return Crew{}, apperr.FieldErrors{}.Add("crew", i18n.T(ctx, "crews.err.blocked-join"))
		}
	}
	limit, err := s.limit(ctx, c.ID, c.OwnerID)
	if err != nil {
		return Crew{}, err
	}
	if c.Members >= limit {
		return Crew{}, apperr.FieldErrors{}.Add("crew", i18n.T(ctx, "crews.err.full"))
	}
	if err = s.repo.AddMember(ctx, c.ID, userID); err != nil {
		return Crew{}, err
	}
	c.Members++
	return c, nil
}

// limit is the crew's size cap, from its owner's tier.
func (s *Service) limit(ctx context.Context, crewID, ownerID uuid.UUID) (int, error) {
	members, err := s.repo.Members(ctx, crewID)
	if err != nil {
		return 0, err
	}
	for _, m := range members {
		if m.ID == ownerID && users.Tier(m.Tier) == users.TierPro {
			return crew.MaxMembersPro, nil
		}
	}
	return crew.MaxMembers, nil
}

// Leave takes the user out. An owner who leaves hands the crew to whoever
// has been in it longest; the last one out deletes it.
func (s *Service) Leave(ctx context.Context, crewID, userID uuid.UUID) error {
	c, err := s.get(ctx, crewID, userID)
	if err != nil {
		return err
	}
	if c.OwnerID == userID {
		next, nextErr := s.repo.OldestOtherMember(ctx, crewID, userID)
		if errors.Is(nextErr, apperr.ErrNotFound) {
			if err = s.repo.DeleteCrew(ctx, crewID); err != nil {
				return err
			}
			return s.removed(ctx, crewID, userID)
		}
		if nextErr != nil {
			return nextErr
		}
		if err = s.repo.SetOwner(ctx, crewID, next); err != nil {
			return err
		}
	}
	if err = s.repo.RemoveMember(ctx, crewID, userID); err != nil {
		return err
	}
	return s.removed(ctx, crewID, userID)
}

// removed tells anyone watching that userID is out of the crew, so their
// own open streams close. The removal is already committed.
func (s *Service) removed(ctx context.Context, crewID, userID uuid.UUID) error {
	return s.repo.Notify(ctx, live.Event{Type: live.EventRemoved, CrewID: crewID, UserID: userID})
}

// Remove is somebody who runs the crew taking somebody out. The owner may
// remove anyone; an admin only plain members.
func (s *Service) Remove(ctx context.Context, crewID, actorID, memberID uuid.UUID) error {
	c, err := s.get(ctx, crewID, actorID)
	if err != nil {
		return err
	}
	actor, err := s.role(ctx, c, actorID)
	if err != nil {
		return err
	}
	if !actor.CanManage() {
		return apperr.ErrNotFound
	}
	if memberID == actorID {
		return s.Leave(ctx, crewID, actorID)
	}
	if actor != crew.RoleOwner {
		target, roleErr := s.role(ctx, c, memberID)
		if roleErr != nil {
			return roleErr
		}
		if target != crew.RoleMember {
			return apperr.ErrNotFound
		}
	}
	if err = s.repo.RemoveMember(ctx, crewID, memberID); err != nil {
		return err
	}
	return s.removed(ctx, crewID, memberID)
}

// SetRole makes a member an admin or a plain member again; only the owner
// may, and ownership itself is never a role to hand out.
func (s *Service) SetRole(ctx context.Context, crewID, ownerID, memberID uuid.UUID, role Role) error {
	c, err := s.get(ctx, crewID, ownerID)
	if err != nil {
		return err
	}
	if c.OwnerID != ownerID {
		return apperr.ErrNotFound
	}
	if role != crew.RoleAdmin && role != crew.RoleMember {
		return apperr.FieldErrors{}.Add("role", "Choose admin or member.")
	}
	if memberID == ownerID {
		return apperr.FieldErrors{}.Add("role", "The owner's role can't change.")
	}
	return s.repo.SetMemberRole(ctx, crewID, memberID, role)
}

// role is what userID is in crew c; not found if they are not in it.
func (s *Service) role(ctx context.Context, c Crew, userID uuid.UUID) (Role, error) {
	if c.OwnerID == userID {
		return crew.RoleOwner, nil
	}
	return s.repo.MemberRole(ctx, c.ID, userID)
}

// Channels are the crew's channels in order, for members only.
func (s *Service) Channels(ctx context.Context, crewID, viewerID uuid.UUID) ([]Channel, error) {
	if _, err := s.get(ctx, crewID, viewerID); err != nil {
		return nil, err
	}
	return s.repo.ListChannels(ctx, crewID)
}

// SetChallenge sets the weekly challenge; the owner and admins may.
func (s *Service) SetChallenge(ctx context.Context, crewID, actorID uuid.UUID, ch *Challenge) error {
	c, err := s.get(ctx, crewID, actorID)
	if err != nil {
		return err
	}
	actor, err := s.role(ctx, c, actorID)
	if err != nil {
		return err
	}
	if !actor.CanManage() {
		return apperr.ErrNotFound
	}
	if ch == nil {
		return s.repo.ClearChallenge(ctx, crewID)
	}
	if ch.Kind != crew.ChallengeCheckIns && ch.Kind != crew.ChallengeWorkouts {
		return apperr.FieldErrors{}.Add("kind", "Choose check-ins or workouts.")
	}
	if ch.Target < 1 || ch.Target > 7 {
		return apperr.FieldErrors{}.Add("target", "Choose between 1 and 7 a week.")
	}
	return s.repo.SetChallenge(ctx, crewID, ch.Kind, ch.Target)
}

func (s *Service) Mine(ctx context.Context, userID uuid.UUID) ([]Crew, error) {
	return s.repo.ListMine(ctx, userID)
}

// get is the crew if the user is in it, not found otherwise.
func (s *Service) get(ctx context.Context, crewID, userID uuid.UUID) (Crew, error) {
	member, err := s.repo.IsMember(ctx, crewID, userID)
	if err != nil {
		return Crew{}, err
	}
	if !member {
		return Crew{}, apperr.ErrNotFound
	}
	return s.repo.GetCrew(ctx, crewID)
}

// Board is the crew as viewer sees it: each member's day, streak, and week.
// Each member's today and week are in their own time zone.
func (s *Service) Board(ctx context.Context, crewID, viewerID uuid.UUID) (Board, error) {
	c, err := s.get(ctx, crewID, viewerID)
	if err != nil {
		return Board{}, err
	}
	rows, err := s.repo.Members(ctx, crewID)
	if err != nil {
		return Board{}, err
	}
	out := Board{
		Crew:    Crew{ID: c.ID, Name: c.Name, OwnerID: c.OwnerID, Code: c.Code, Members: len(rows)},
		IsOwner: c.OwnerID == viewerID,
	}
	if out.Challenge, err = s.repo.Challenge(ctx, crewID); err != nil {
		return Board{}, err
	}

	// Each member's signals are a few queries; a crew can be a hundred
	// people, so fill a few at a time rather than one after another.
	now := s.now()
	out.Members = make([]Member, len(rows))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(boardFillers)
	for i, r := range rows {
		m := Member{ID: r.ID, DisplayName: r.DisplayName, Owner: r.ID == c.OwnerID, Me: r.ID == viewerID, Role: r.Role}
		if m.Owner {
			m.Role = crew.RoleOwner
		}
		m.Handle = util.Val(r.Handle)
		u := users.User{ID: r.ID, Timezone: r.Timezone}
		if m.Me {
			out.WeekStart = weekStart(now.In(u.Location()))
			out.MyRole = m.Role
		}
		out.Members[i] = m
		g.Go(func() error { return s.fill(gctx, &out.Members[i], u, now, out.Challenge) })
	}
	if err = g.Wait(); err != nil {
		return Board{}, err
	}
	return out, nil
}

// boardFillers is how many members' signals the board reads at once. The
// pool holds ten connections, so a board never takes more than a few of them.
const boardFillers = 4

func (s *Service) fill(ctx context.Context, m *Member, u users.User, now time.Time, ch *Challenge) error {
	local := now.In(u.Location())
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, u.Location())
	var err error
	if s.checkIns != nil {
		if m.CheckedIn, err = s.checkIns.CheckedInOn(ctx, u.ID, today); err != nil {
			return err
		}
		if m.Streak, err = s.checkIns.Streak(ctx, u, now); err != nil {
			return err
		}
	}
	if s.workouts != nil {
		n, countErr := s.workouts.CountBetween(ctx, u.ID, today, today.AddDate(0, 0, 1))
		if countErr != nil {
			return countErr
		}
		m.WorkedOut = n > 0
	}
	if ch != nil {
		from := weekStart(local)
		m.WeekProgress, err = s.progress(ctx, u.ID, ch.Kind, from, from.AddDate(0, 0, 7))
	}
	return err
}

// weekStart is the Monday 00:00 of the week containing t, in t's zone.
func weekStart(t time.Time) time.Time {
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	offset := (int(day.Weekday()) + 6) % 7 // Monday = 0
	return day.AddDate(0, 0, -offset)
}

// Crewmates are everybody sharing a crew with the user, for the evening
// nudge. Names only.
func (s *Service) Crewmates(ctx context.Context, userID uuid.UUID) ([]Member, error) {
	rows, err := s.repo.CrewmatesOf(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]Member, 0, len(rows))
	for _, r := range rows {
		out = append(out, Member{ID: r.ID, DisplayName: r.DisplayName})
	}
	return out, nil
}

const codeAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

func newCode() string {
	limit := byte(256 - 256%len(codeAlphabet))
	out := make([]byte, 0, 10)
	buf := make([]byte, 16)
	for len(out) < 10 {
		if _, err := rand.Read(buf); err != nil {
			panic(err)
		}
		for _, b := range buf {
			if b < limit && len(out) < 10 {
				out = append(out, codeAlphabet[int(b)%len(codeAlphabet)])
			}
		}
	}
	return string(out)
}

// CheckedInCrewmates are the names of the people sharing a crew with user who
// have checked in today, each in their own day. For the evening nudge.
func (s *Service) CheckedInCrewmates(ctx context.Context, user users.User, now time.Time) ([]string, error) {
	if s.checkIns == nil {
		return nil, nil
	}
	rows, err := s.repo.CrewmatesOf(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, r := range rows {
		mate := users.User{ID: r.ID, Timezone: r.Timezone}
		local := now.In(mate.Location())
		ok, err := s.checkIns.CheckedInOn(ctx, r.ID, time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, mate.Location()))
		if err != nil {
			return nil, err
		}
		if ok {
			names = append(names, r.DisplayName)
		}
	}
	return names, nil
}
