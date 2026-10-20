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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/sync/errgroup"

	"github.com/NorthAIProject/north-client/internal/crews/crew"
	crewsdb "github.com/NorthAIProject/north-client/internal/crews/db"
	"github.com/NorthAIProject/north-client/internal/crews/live"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

type (
	Crew      = crew.Crew
	Challenge = crew.Challenge
	Member    = crew.Member
	Board     = crew.Board
	Role      = crew.Role
	Channel   = crew.Channel
)

type Service struct {
	pool     *pgxpool.Pool
	q        *crewsdb.Queries
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
// *social.Repository is one; it is narrowed here so crews does not depend on
// the social slice.
type BlockChecker interface {
	Blocked(ctx context.Context, a, b uuid.UUID) (bool, error)
}

func NewService(pool *pgxpool.Pool, checkIns CheckInReader, workouts WorkoutReader) *Service {
	return &Service{pool: pool, q: crewsdb.New(pool), checkIns: checkIns, workouts: workouts, now: time.Now}
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
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Crew{}, apperr.Wrap(err, "begin create crew")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	row, err := q.CreateCrew(ctx, crewsdb.CreateCrewParams{Name: name, OwnerID: ownerID, Code: newCode()})
	if err != nil {
		return Crew{}, apperr.Wrap(err, "create crew")
	}
	if _, err = q.AddMember(ctx, crewsdb.AddMemberParams{CrewID: row.ID, UserID: ownerID}); err != nil {
		return Crew{}, apperr.Wrap(err, "add owner")
	}
	if err = q.CreateDefaultChannels(ctx, row.ID); err != nil {
		return Crew{}, apperr.Wrap(err, "create default channels")
	}
	general, err := q.DefaultChannel(ctx, row.ID)
	if err != nil {
		return Crew{}, apperr.Wrap(err, "default channel")
	}
	if err = q.InsertSystemMessage(ctx, crewsdb.InsertSystemMessageParams{CrewID: row.ID, ChannelID: general, Body: "crews.sys.welcome"}); err != nil {
		return Crew{}, apperr.Wrap(err, "welcome message")
	}
	if err = tx.Commit(ctx); err != nil {
		return Crew{}, apperr.Wrap(err, "commit create crew")
	}
	return Crew{ID: row.ID, Name: row.Name, OwnerID: row.OwnerID, Code: row.Code, Members: 1}, nil
}

// Preview is the crew behind a join code, for the join page.
func (s *Service) Preview(ctx context.Context, code string) (Crew, error) {
	row, err := s.q.CrewByCode(ctx, strings.ToLower(strings.TrimSpace(code)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Crew{}, apperr.ErrNotFound
	}
	if err != nil {
		return Crew{}, apperr.Wrap(err, "crew by code")
	}
	n, err := s.q.CountMembers(ctx, row.ID)
	if err != nil {
		return Crew{}, apperr.Wrap(err, "count members")
	}
	return Crew{ID: row.ID, Name: row.Name, OwnerID: row.OwnerID, Code: row.Code, Members: int(n)}, nil
}

// Join adds the user to the crew behind code, if it has room. Joining twice
// is a no-op.
func (s *Service) Join(ctx context.Context, userID uuid.UUID, code string) (Crew, error) {
	c, err := s.Preview(ctx, code)
	if err != nil {
		return Crew{}, err
	}
	member, err := s.q.IsMember(ctx, crewsdb.IsMemberParams{CrewID: c.ID, UserID: userID})
	if err != nil {
		return Crew{}, apperr.Wrap(err, "check membership")
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
			return Crew{}, apperr.FieldErrors{}.Add("crew", "You can't join this crew.")
		}
	}
	limit, err := s.limit(ctx, c.ID, c.OwnerID)
	if err != nil {
		return Crew{}, err
	}
	if c.Members >= limit {
		return Crew{}, apperr.FieldErrors{}.Add("crew", "This crew is full.")
	}
	if _, err = s.q.AddMember(ctx, crewsdb.AddMemberParams{CrewID: c.ID, UserID: userID}); err != nil {
		return Crew{}, apperr.Wrap(err, "join crew")
	}
	c.Members++
	return c, nil
}

// limit is the crew's size cap, from its owner's tier.
func (s *Service) limit(ctx context.Context, crewID, ownerID uuid.UUID) (int, error) {
	members, err := s.q.Members(ctx, crewID)
	if err != nil {
		return 0, apperr.Wrap(err, "list members")
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
		next, nextErr := s.q.OldestOtherMember(ctx, crewsdb.OldestOtherMemberParams{CrewID: crewID, UserID: userID})
		if errors.Is(nextErr, pgx.ErrNoRows) {
			if err = s.q.DeleteCrew(ctx, crewID); err != nil {
				return apperr.Wrap(err, "delete crew")
			}
			return s.removed(ctx, crewID, userID)
		}
		if nextErr != nil {
			return apperr.Wrap(nextErr, "find next owner")
		}
		if err = s.q.SetOwner(ctx, crewsdb.SetOwnerParams{ID: crewID, OwnerID: next}); err != nil {
			return apperr.Wrap(err, "hand over crew")
		}
	}
	if _, err = s.q.RemoveMember(ctx, crewsdb.RemoveMemberParams{CrewID: crewID, UserID: userID}); err != nil {
		return apperr.Wrap(err, "remove member")
	}
	return s.removed(ctx, crewID, userID)
}

// removed tells anyone watching that userID is out of the crew, so their
// own open streams close. The removal is already committed.
func (s *Service) removed(ctx context.Context, crewID, userID uuid.UUID) error {
	return live.Notify(ctx, s.pool, live.Event{Type: live.EventRemoved, CrewID: crewID, UserID: userID})
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
	if _, err = s.q.RemoveMember(ctx, crewsdb.RemoveMemberParams{CrewID: crewID, UserID: memberID}); err != nil {
		return apperr.Wrap(err, "remove member")
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
	n, err := s.q.SetMemberRole(ctx, crewsdb.SetMemberRoleParams{CrewID: crewID, UserID: memberID, Role: string(role)})
	if err != nil {
		return apperr.Wrap(err, "set role")
	}
	if n == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

// role is what userID is in crew c; not found if they are not in it.
func (s *Service) role(ctx context.Context, c crewsdb.Crew, userID uuid.UUID) (Role, error) {
	if c.OwnerID == userID {
		return crew.RoleOwner, nil
	}
	r, err := s.q.MemberRole(ctx, crewsdb.MemberRoleParams{CrewID: c.ID, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", apperr.ErrNotFound
	}
	if err != nil {
		return "", apperr.Wrap(err, "member role")
	}
	return Role(r), nil
}

// Channels are the crew's channels in order, for members only.
func (s *Service) Channels(ctx context.Context, crewID, viewerID uuid.UUID) ([]Channel, error) {
	if _, err := s.get(ctx, crewID, viewerID); err != nil {
		return nil, err
	}
	rows, err := s.q.ListChannels(ctx, crewID)
	if err != nil {
		return nil, apperr.Wrap(err, "list channels")
	}
	out := make([]Channel, 0, len(rows))
	for _, r := range rows {
		out = append(out, Channel{ID: r.ID, Kind: r.Kind, Name: r.Name, Topic: r.Topic, Position: int(r.Position), IsDefault: r.IsDefault})
	}
	return out, nil
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
		return s.q.ClearChallenge(ctx, crewID)
	}
	if ch.Kind != crew.ChallengeCheckIns && ch.Kind != crew.ChallengeWorkouts {
		return apperr.FieldErrors{}.Add("kind", "Choose check-ins or workouts.")
	}
	if ch.Target < 1 || ch.Target > 7 {
		return apperr.FieldErrors{}.Add("target", "Choose between 1 and 7 a week.")
	}
	return s.q.SetChallenge(ctx, crewsdb.SetChallengeParams{CrewID: crewID, Kind: ch.Kind, Target: int16(ch.Target)})
}

func (s *Service) Mine(ctx context.Context, userID uuid.UUID) ([]Crew, error) {
	rows, err := s.q.ListMine(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "list crews")
	}
	out := make([]Crew, 0, len(rows))
	for _, r := range rows {
		out = append(out, Crew{ID: r.ID, Name: r.Name, OwnerID: r.OwnerID, Code: r.Code, Members: int(r.Members)})
	}
	return out, nil
}

// get is the crew if the user is in it, not found otherwise.
func (s *Service) get(ctx context.Context, crewID, userID uuid.UUID) (crewsdb.Crew, error) {
	member, err := s.q.IsMember(ctx, crewsdb.IsMemberParams{CrewID: crewID, UserID: userID})
	if err != nil {
		return crewsdb.Crew{}, apperr.Wrap(err, "check membership")
	}
	if !member {
		return crewsdb.Crew{}, apperr.ErrNotFound
	}
	c, err := s.q.GetCrew(ctx, crewID)
	if err != nil {
		return crewsdb.Crew{}, apperr.Wrap(err, "get crew")
	}
	return c, nil
}

// Board is the crew as viewer sees it: each member's day, streak, and week.
// Each member's today and week are in their own time zone.
func (s *Service) Board(ctx context.Context, crewID, viewerID uuid.UUID) (Board, error) {
	c, err := s.get(ctx, crewID, viewerID)
	if err != nil {
		return Board{}, err
	}
	rows, err := s.q.Members(ctx, crewID)
	if err != nil {
		return Board{}, apperr.Wrap(err, "list members")
	}
	out := Board{
		Crew:    Crew{ID: c.ID, Name: c.Name, OwnerID: c.OwnerID, Code: c.Code, Members: len(rows)},
		IsOwner: c.OwnerID == viewerID,
	}
	ch, err := s.q.GetChallenge(ctx, crewID)
	switch {
	case err == nil:
		out.Challenge = &Challenge{Kind: ch.Kind, Target: int(ch.Target)}
	case !errors.Is(err, pgx.ErrNoRows):
		return Board{}, apperr.Wrap(err, "get challenge")
	}

	// Each member's signals are a few queries; a crew can be a hundred
	// people, so fill a few at a time rather than one after another.
	now := s.now()
	out.Members = make([]Member, len(rows))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(boardFillers)
	for i, r := range rows {
		m := Member{ID: r.ID, DisplayName: r.DisplayName, Owner: r.ID == c.OwnerID, Me: r.ID == viewerID, Role: Role(r.Role)}
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
	rows, err := s.q.CrewmatesOf(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "crewmates")
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
	rows, err := s.q.CrewmatesOf(ctx, user.ID)
	if err != nil {
		return nil, apperr.Wrap(err, "crewmates")
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
