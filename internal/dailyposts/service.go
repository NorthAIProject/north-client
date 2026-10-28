// Package dailyposts is the daily post: one short piece on wellness, food or
// wellbeing that the model writes once a UTC day and everyone reads, with
// kudos and a way into the coach.
package dailyposts

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/dailyposts/post"
)

// archiveDays is how many posts the page shows, today's included.
const archiveDays = 14

type Service struct {
	repo Repository
	now  func() time.Time
}

// Repository is the persistence Service and Writer need. They name it rather
// than the Postgres type so the database stays a detail of the composition
// root: NewRepository is what the process wires, and a test can hand them
// anything that keeps daily posts and their kudos.
type Repository interface {
	Insert(ctx context.Context, d Draft) (bool, error)
	Exists(ctx context.Context, day time.Time) (bool, error)
	Recent(ctx context.Context, viewer uuid.UUID, day time.Time, limit int32) ([]post.Post, error)
	EarlierThemes(ctx context.Context, day time.Time, limit int32) ([]Earlier, error)
	GiveKudos(ctx context.Context, postID, userID uuid.UUID) error
	TakeKudos(ctx context.Context, postID, userID uuid.UUID) error
	Week(ctx context.Context, since, until time.Time) (Aggregate, error)
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo, now: time.Now}
}

// WithClock replaces the clock, for tests.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// Feed is today's post, when there is one, and the ones before it.
type Feed struct {
	Today   *post.Post
	Earlier []post.Post
}

func (s *Service) Feed(ctx context.Context, viewer uuid.UUID) (Feed, error) {
	today := s.now().UTC()
	posts, err := s.repo.Recent(ctx, viewer, today, archiveDays)
	if err != nil {
		return Feed{}, err
	}
	var f Feed
	if len(posts) > 0 && sameDay(posts[0].Date, today) {
		f.Today = &posts[0]
		posts = posts[1:]
	}
	f.Earlier = posts
	return f, nil
}

// GiveKudos is idempotent; ErrNotFound when the post does not exist.
func (s *Service) GiveKudos(ctx context.Context, postID, viewer uuid.UUID) error {
	return s.repo.GiveKudos(ctx, postID, viewer)
}

func (s *Service) TakeKudos(ctx context.Context, postID, viewer uuid.UUID) error {
	return s.repo.TakeKudos(ctx, postID, viewer)
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}
