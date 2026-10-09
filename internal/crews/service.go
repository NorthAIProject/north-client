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

	"github.com/NorthAIProject/north-client/internal/crews/crew"
	crewsdb "github.com/NorthAIProject/north-client/internal/crews/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

type (
	Crew      = crew.Crew
	Challenge = crew.Challenge
	Member    = crew.Member
	Board     = crew.Board
)

type Service struct {
	pool     *pgxpool.Pool
	q        *crewsdb.Queries
	checkIns CheckInReader
	workouts WorkoutReader
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

func NewService(pool *pgxpool.Pool, checkIns CheckInReader, workouts WorkoutReader) *Service {
	return &Service{pool: pool, q: crewsdb.New(pool), checkIns: checkIns, workouts: workouts, now: time.Now}
}

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
			return s.q.DeleteCrew(ctx, crewID)
		}
		if nextErr != nil {
			return apperr.Wrap(nextErr, "find next owner")
		}
		if err = s.q.SetOwner(ctx, crewsdb.SetOwnerParams{ID: crewID, OwnerID: next}); err != nil {
			return apperr.Wrap(err, "hand over crew")
		}
	}
	_, err = s.q.RemoveMember(ctx, crewsdb.RemoveMemberParams{CrewID: crewID, UserID: userID})
	return err
}

// Remove is the owner taking somebody out.
func (s *Service) Remove(ctx context.Context, crewID, ownerID, memberID uuid.UUID) error {
	c, err := s.get(ctx, crewID, ownerID)
	if err != nil {
		return err
	}
	if c.OwnerID != ownerID {
		return apperr.ErrNotFound
	}
	if memberID == ownerID {
		return s.Leave(ctx, crewID, ownerID)
	}
	_, err = s.q.RemoveMember(ctx, crewsdb.RemoveMemberParams{CrewID: crewID, UserID: memberID})
	return err
}

// SetChallenge sets the weekly challenge; only the owner may.
func (s *Service) SetChallenge(ctx context.Context, crewID, ownerID uuid.UUID, ch *Challenge) error {
	c, err := s.get(ctx, crewID, ownerID)
	if err != nil {
		return err
	}
	if c.OwnerID != ownerID {
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

	now := s.now()
	for _, r := range rows {
		u := users.User{ID: r.ID, Timezone: r.Timezone}
		m := Member{ID: r.ID, DisplayName: r.DisplayName, Owner: r.ID == c.OwnerID, Me: r.ID == viewerID}
		m.Handle = util.Val(r.Handle)
		if err := s.fill(ctx, &m, u, now, out.Challenge); err != nil {
			return Board{}, err
		}
		if m.Me {
			out.WeekStart = weekStart(now.In(u.Location()))
		}
		out.Members = append(out.Members, m)
	}
	return out, nil
}

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
