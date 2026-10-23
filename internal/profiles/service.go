// Package profiles is what one person may see of another: the public profile,
// and the finished workouts and level the owner has chosen to share.
//
// Anyone signed in and not blocked can open a profile; each section on it is
// gated by the owner's own sharing switch, and the owner always sees all of
// their own. Everything that reads another person's data passes through this
// package's checks first.
package profiles

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/achievements"
	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/awards"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/social"
	"github.com/NorthAIProject/north-client/internal/users"
	"github.com/NorthAIProject/north-client/internal/xp"
)

const (
	// PageSize is how many workouts a page of the list holds.
	PageSize = 20
	// RecentCount is how many workouts the profile itself shows.
	RecentCount = 5
)

// People opens a profile; an unknown handle and a block are the same not found.
type People interface {
	Profile(ctx context.Context, viewerID uuid.UUID, handle string) (social.Profile, error)
	HandleOf(ctx context.Context, userID uuid.UUID) (string, error)
}

// Sharing is the owner's switches.
type Sharing interface {
	Sharing(ctx context.Context, userID uuid.UUID) (achievements.Sharing, error)
}

// Levels reads a person's level, whoever asks.
type Levels interface {
	LevelOf(ctx context.Context, userID uuid.UUID, now time.Time) (xp.Level, error)
}

// Sessions reads a person's finished workouts.
type Sessions interface {
	CountCompleted(ctx context.Context, userID uuid.UUID) (int, error)
	ListCompleted(ctx context.Context, userID uuid.UUID, before time.Time, limit int) ([]activity.Session, error)
}

// Cards builds workout cards. It does not check the reader.
type Cards interface {
	CardsFor(ctx context.Context, sessionIDs []uuid.UUID) (map[uuid.UUID]lift.Card, error)
}

// Badges reads the badges a person holds, whoever asks.
type Badges interface {
	Earned(ctx context.Context, userID uuid.UUID) ([]awards.Earned, error)
}

// Shares is what this viewer may see.
type Shares struct{ Training, XP, Awards bool }

// View is one profile as one viewer sees it.
type View struct {
	Profile  social.Profile // person, follower/following counts, relationship
	Shares   Shares
	Level    *xp.Level   // nil unless Shares.XP
	Workouts *int        // completed count; nil unless Shares.Training
	Recent   []lift.Card // newest 5; nil unless Shares.Training
	// Awards are the badges held, oldest first; nil unless Shares.Awards.
	// Another person's never include a private one.
	Awards []awards.Earned
}

// Page is one page of a person's workouts, newest first.
type Page struct {
	Items []lift.Card
	// Before is the EndedAt of the last item when a full page came back.
	Before *time.Time
	Shared bool
}

type Service struct {
	people   People
	sharing  Sharing
	levels   Levels
	sessions Sessions
	cards    Cards
	badges   Badges
}

func NewService(people People, sharing Sharing, levels Levels, sessions Sessions, cards Cards) *Service {
	return &Service{people: people, sharing: sharing, levels: levels, sessions: sessions, cards: cards}
}

// WithAwards lets the profile show badges.
func (s *Service) WithAwards(b Badges) *Service { s.badges = b; return s }

// open resolves the handle for the viewer and works out what they may see.
// Nothing about the owner's data is read before this returns.
func (s *Service) open(ctx context.Context, viewer users.User, handle string) (social.Profile, Shares, error) {
	p, err := s.people.Profile(ctx, viewer.ID, handle)
	if err != nil {
		return social.Profile{}, Shares{}, err
	}
	if p.Relationship.Self {
		return p, Shares{Training: true, XP: true, Awards: true}, nil
	}
	sh, err := s.sharing.Sharing(ctx, p.ID)
	if err != nil {
		return social.Profile{}, Shares{}, err
	}
	return p, Shares{Training: sh.Training, XP: sh.XP, Awards: sh.Awards}, nil
}

// Person is who the awards page is about and whether the viewer may see their
// badges. A block or an unknown handle is ErrNotFound, as for the profile.
func (s *Service) Person(ctx context.Context, viewer users.User, handle string) (awards.Person, error) {
	p, shares, err := s.open(ctx, viewer, handle)
	if err != nil {
		return awards.Person{}, err
	}
	return awards.Person{ID: p.ID, Name: p.DisplayName, Handle: p.Handle, Self: p.Relationship.Self, Shared: shares.Awards}, nil
}

// HandleOf is the person's handle, empty when they have none.
func (s *Service) HandleOf(ctx context.Context, userID uuid.UUID) (string, error) {
	return s.people.HandleOf(ctx, userID)
}

// View is the profile for handle as viewer sees it. A block or an unknown
// handle is ErrNotFound.
func (s *Service) View(ctx context.Context, viewer users.User, handle string, now time.Time) (View, error) {
	p, shares, err := s.open(ctx, viewer, handle)
	if err != nil {
		return View{}, err
	}
	v := View{Profile: p, Shares: shares}
	if shares.XP {
		l, err := s.levels.LevelOf(ctx, p.ID, now)
		if err != nil {
			return View{}, err
		}
		v.Level = &l
	}
	if shares.Awards && s.badges != nil {
		held, err := s.badges.Earned(ctx, p.ID)
		if err != nil {
			return View{}, err
		}
		for _, e := range held {
			// Only the owner sees a private badge, whatever the switch.
			if p.Relationship.Self || !e.Badge.Area.Private() {
				v.Awards = append(v.Awards, e)
			}
		}
	}
	if shares.Training {
		n, err := s.sessions.CountCompleted(ctx, p.ID)
		if err != nil {
			return View{}, err
		}
		v.Workouts = &n
		recent, err := s.page(ctx, p.ID, now, RecentCount)
		if err != nil {
			return View{}, err
		}
		v.Recent = recent
	}
	return v, nil
}

// Workouts is a page of the person's finished workouts that ended before
// before (zero means now). When training is not shared the page is empty and
// Shared is false.
func (s *Service) Workouts(ctx context.Context, viewer users.User, handle string, before time.Time) (Page, error) {
	p, shares, err := s.open(ctx, viewer, handle)
	if err != nil {
		return Page{}, err
	}
	if !shares.Training {
		return Page{Items: []lift.Card{}}, nil
	}
	if before.IsZero() {
		before = time.Now()
	}
	items, err := s.page(ctx, p.ID, before, PageSize)
	if err != nil {
		return Page{}, err
	}
	out := Page{Items: items, Shared: true}
	if len(items) == PageSize {
		end := items[len(items)-1].EndedAt
		out.Before = &end
	}
	return out, nil
}

// page is the owner's newest limit finished workouts before a point, as cards.
func (s *Service) page(ctx context.Context, ownerID uuid.UUID, before time.Time, limit int) ([]lift.Card, error) {
	sessions, err := s.sessions.ListCompleted(ctx, ownerID, before, limit)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(sessions))
	for i, se := range sessions {
		ids[i] = se.ID
	}
	byID, err := s.cards.CardsFor(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]lift.Card, 0, len(sessions))
	for _, se := range sessions {
		// Defence in depth: the list is the owner's, but a card is only ever
		// handed out under the owner's own id.
		if c, ok := byID[se.ID]; ok && c.UserID == ownerID {
			out = append(out, c)
		}
	}
	return out, nil
}

// Workout is one finished workout of the handle's person. It is ErrNotFound
// when the viewer is blocked, training is not shared, the session is missing
// or not completed, or it belongs to somebody else.
func (s *Service) Workout(ctx context.Context, viewer users.User, handle string, sessionID uuid.UUID) (lift.Card, error) {
	p, shares, err := s.open(ctx, viewer, handle)
	if err != nil {
		return lift.Card{}, err
	}
	if !shares.Training {
		return lift.Card{}, apperr.ErrNotFound
	}
	byID, err := s.cards.CardsFor(ctx, []uuid.UUID{sessionID})
	if err != nil {
		return lift.Card{}, err
	}
	c, ok := byID[sessionID]
	if !ok || c.UserID != p.ID {
		return lift.Card{}, apperr.ErrNotFound
	}
	return c, nil
}
