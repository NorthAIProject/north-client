package dailyposts

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	dailypostsdb "github.com/NorthAIProject/north-client/internal/dailyposts/db"
	"github.com/NorthAIProject/north-client/internal/dailyposts/post"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *dailypostsdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: dailypostsdb.New(pool)}
}

// Draft is a post ready to publish.
type Draft struct {
	Date          time.Time
	Theme         string
	Title         string
	Body          string
	Model         string
	PromptVersion string
}

// Insert publishes the day's post. inserted is false when that day already
// has one: the once-a-day limit.
func (r *Repository) Insert(ctx context.Context, d Draft) (bool, error) {
	_, err := r.q.InsertPost(ctx, dailypostsdb.InsertPostParams{
		PostDate: date(d.Date), Theme: d.Theme, Title: d.Title, Body: d.Body,
		AiGenerated: true, Model: d.Model, PromptVersion: d.PromptVersion,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, apperr.Wrap(err, "insert daily post")
	}
	return true, nil
}

func (r *Repository) Exists(ctx context.Context, day time.Time) (bool, error) {
	ok, err := r.q.PostExists(ctx, date(day))
	if err != nil {
		return false, apperr.Wrap(err, "check daily post")
	}
	return ok, nil
}

// Recent is up to limit posts on or before day, newest first.
func (r *Repository) Recent(ctx context.Context, viewer uuid.UUID, day time.Time, limit int32) ([]post.Post, error) {
	rows, err := r.q.RecentPosts(ctx, dailypostsdb.RecentPostsParams{
		Viewer: viewer, OnOrBefore: date(day), Lim: limit,
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list daily posts")
	}
	out := make([]post.Post, 0, len(rows))
	for _, row := range rows {
		out = append(out, post.Post{
			ID: row.ID, Date: row.PostDate.Time, Theme: row.Theme, Title: row.Title, Body: row.Body,
			AIGenerated: row.AiGenerated, Kudos: int(row.Kudos), Kudoed: row.Kudoed,
		})
	}
	return out, nil
}

// Earlier is what the posts before day were about, newest first.
type Earlier struct {
	Theme string
	Title string
}

func (r *Repository) EarlierThemes(ctx context.Context, day time.Time, limit int32) ([]Earlier, error) {
	rows, err := r.q.RecentPostThemes(ctx, dailypostsdb.RecentPostThemesParams{
		Before: date(day), Lim: limit,
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list earlier daily posts")
	}
	out := make([]Earlier, 0, len(rows))
	for _, row := range rows {
		out = append(out, Earlier{Theme: row.Theme, Title: row.Title})
	}
	return out, nil
}

func (r *Repository) GiveKudos(ctx context.Context, postID, userID uuid.UUID) error {
	ok, err := r.q.PostIDExists(ctx, postID)
	if err != nil {
		return apperr.Wrap(err, "find daily post")
	}
	if !ok {
		return apperr.ErrNotFound
	}
	if err := r.q.GiveKudos(ctx, dailypostsdb.GiveKudosParams{PostID: postID, UserID: userID}); err != nil {
		return apperr.Wrap(err, "give kudos")
	}
	return nil
}

func (r *Repository) TakeKudos(ctx context.Context, postID, userID uuid.UUID) error {
	if err := r.q.TakeKudos(ctx, dailypostsdb.TakeKudosParams{PostID: postID, UserID: userID}); err != nil {
		return apperr.Wrap(err, "take kudos")
	}
	return nil
}

// Week is everyone's logs over a window, as counts and averages.
func (r *Repository) Week(ctx context.Context, since, until time.Time) (Aggregate, error) {
	ci, err := r.q.WeekCheckIns(ctx, dailypostsdb.WeekCheckInsParams{Since: date(since), Until: date(until)})
	if err != nil {
		return Aggregate{}, apperr.Wrap(err, "aggregate check-ins")
	}
	sl, err := r.q.WeekSleep(ctx, dailypostsdb.WeekSleepParams{Since: date(since), Until: date(until)})
	if err != nil {
		return Aggregate{}, apperr.Wrap(err, "aggregate sleep")
	}
	return Aggregate{
		CheckInPeople: int(ci.People), AvgEnergy: ci.AvgEnergy, AvgMood: ci.AvgMood,
		SleepPeople: int(sl.People), AvgSleepMinutes: sl.AvgMinutes,
	}, nil
}

func date(t time.Time) pgtype.Date {
	return pgtype.Date{Time: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
}
