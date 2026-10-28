package achievements

import (
	"context"
	"errors"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	achievementsdb "github.com/NorthAIProject/north-client/internal/achievements/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// PostgresRepository keeps achievements, sharing switches and kudos in
// Postgres. It is the Repository the process wires into Service.
type PostgresRepository struct {
	q *achievementsdb.Queries
}

var _ Repository = (*PostgresRepository)(nil)

func NewRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{q: achievementsdb.New(pool)}
}

// Record stores a moment once. One already recorded under the same source
// key is not an error.
func (r *PostgresRepository) Record(ctx context.Context, userID uuid.UUID, category, kind, sourceKey, title, detail string, at time.Time) error {
	_, err := r.q.Record(ctx, achievementsdb.RecordParams{
		UserID: userID, Category: category, Kind: kind, Title: title, Detail: detail,
		OccurredAt: at, SourceKey: sourceKey,
	})
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	return nil
}

// Post stores a shared moment, refreshing title and detail when the source
// key was posted before.
func (r *PostgresRepository) Post(ctx context.Context, userID uuid.UUID, category, kind, sourceKey, title, detail string, at time.Time) (Item, error) {
	row, err := r.q.Post(ctx, achievementsdb.PostParams{
		UserID: userID, Category: category, Kind: kind, Title: title, Detail: detail,
		OccurredAt: at, SourceKey: sourceKey,
	})
	if err != nil {
		return Item{}, apperr.Wrap(err, "post achievement")
	}
	return Item{
		ID: row.ID, UserID: userID, Category: category, Kind: kind, Title: title, Detail: detail,
		OccurredAt: row.OccurredAt, Mine: true,
	}, nil
}

// OwnBetween is the person's own achievements in [since, until), newest
// first, one item per row.
func (r *PostgresRepository) OwnBetween(ctx context.Context, userID uuid.UUID, since, until time.Time) ([]Item, error) {
	rows, err := r.q.OwnAchievementsBetween(ctx, achievementsdb.OwnAchievementsBetweenParams{
		UserID: userID, Since: since, Until: until,
	})
	if err != nil {
		return nil, apperr.Wrap(err, "own achievements")
	}
	out := make([]Item, 0, len(rows))
	for _, row := range rows {
		out = append(out, Item{
			ID: row.ID, UserID: userID, Category: row.Category, Kind: row.Kind, Title: row.Title, Detail: row.Detail,
			OccurredAt: row.OccurredAt, Mine: true, SourceKey: row.SourceKey,
			MonthlyMetric: util.Val(row.MonthlyMetric), MonthlyTarget: int(util.Val(row.MonthlyTarget)),
		})
	}
	return out, nil
}

// Feed is up to limit achievements the viewer may see, strictly before
// before, newest first, one item per row.
func (r *PostgresRepository) Feed(ctx context.Context, viewerID uuid.UUID, before time.Time, limit int) ([]Item, error) {
	rows, err := r.q.Feed(ctx, achievementsdb.FeedParams{Viewer: viewerID, Before: before, Lim: int32(limit)})
	if err != nil {
		return nil, apperr.Wrap(err, "feed")
	}
	out := make([]Item, 0, len(rows))
	for _, row := range rows {
		out = append(out, Item{
			ID: row.ID, UserID: row.UserID, DisplayName: row.DisplayName, Handle: util.Val(row.Handle),
			Category: row.Category, Kind: row.Kind, Title: row.Title, Detail: row.Detail, OccurredAt: row.OccurredAt,
			Kudos: int(row.Kudos), Kudoed: row.Kudoed, Mine: row.UserID == viewerID, SourceKey: row.SourceKey,
			Private: row.Private, MonthlyMetric: util.Val(row.MonthlyMetric), MonthlyTarget: int(util.Val(row.MonthlyTarget)),
		})
	}
	return out, nil
}

// Sharing is the switches the person has set, by category. A category never
// set is absent.
func (r *PostgresRepository) Sharing(ctx context.Context, userID uuid.UUID) (map[string]bool, error) {
	rows, err := r.q.ListSharing(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "list sharing")
	}
	out := make(map[string]bool, len(rows))
	for _, row := range rows {
		out[row.Category] = row.Shared
	}
	return out, nil
}

func (r *PostgresRepository) SetSharing(ctx context.Context, userID uuid.UUID, category string, shared bool) error {
	if err := r.q.SetSharing(ctx, achievementsdb.SetSharingParams{UserID: userID, Category: category, Shared: shared}); err != nil {
		return apperr.Wrap(err, "set sharing")
	}
	return nil
}

// Visible is the owner and title of an achievement the viewer may see, or
// apperr.ErrNotFound.
func (r *PostgresRepository) Visible(ctx context.Context, achievementID, viewerID uuid.UUID) (Item, error) {
	row, err := r.q.Visible(ctx, achievementsdb.VisibleParams{ID: achievementID, Viewer: viewerID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, apperr.ErrNotFound
	}
	if err != nil {
		return Item{}, apperr.Wrap(err, "check achievement")
	}
	return Item{ID: achievementID, UserID: row.UserID, Title: row.Title}, nil
}

// GiveKudos reports whether the kudos is new.
func (r *PostgresRepository) GiveKudos(ctx context.Context, achievementID, giverID uuid.UUID) (bool, error) {
	added, err := r.q.GiveKudos(ctx, achievementsdb.GiveKudosParams{AchievementID: achievementID, UserID: giverID})
	if err != nil {
		return false, apperr.Wrap(err, "give kudos")
	}
	return added > 0, nil
}

func (r *PostgresRepository) TakeKudos(ctx context.Context, achievementID, giverID uuid.UUID) error {
	if err := r.q.TakeKudos(ctx, achievementsdb.TakeKudosParams{AchievementID: achievementID, UserID: giverID}); err != nil {
		return apperr.Wrap(err, "take kudos")
	}
	return nil
}
