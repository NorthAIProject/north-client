package progresschecks

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/progresschecks/db"
	"github.com/NorthAIProject/north-client/internal/progresschecks/progresscheck"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	pool *pgxpool.Pool
	q    *progresschecksdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool, q: progresschecksdb.New(pool)}
}

// Create stores a check and its photos in one transaction, so a check never
// exists with half its photos.
func (r *Repository) Create(ctx context.Context, userID uuid.UUID, in Input, weightCarried bool, photos []Photo) (Check, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Check{}, apperr.Wrap(err, "begin progress check")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := r.q.WithTx(tx)

	var confidence *string
	if in.AssistConfidence != "" {
		confidence = &in.AssistConfidence
	}
	row, err := q.CreateCheck(ctx, progresschecksdb.CreateCheckParams{
		UserID: userID, MeasuredOn: date(in.MeasuredOn),
		WeightKg: in.WeightKg, WeightCarried: weightCarried,
		WaistCm: in.WaistCm, HipsCm: in.HipsCm, ChestCm: in.ChestCm, ThighCm: in.ThighCm, ArmCm: in.ArmCm,
		Source: in.Source, AssistConfidence: confidence, AssistNotes: in.AssistNotes,
	})
	if err != nil {
		return Check{}, apperr.Wrap(err, "create progress check")
	}
	for _, p := range photos {
		if err := q.AddCheckPhoto(ctx, progresschecksdb.AddCheckPhotoParams{
			CheckID: row.ID, MediaID: p.MediaID, Pose: string(p.Pose),
		}); err != nil {
			return Check{}, apperr.Wrap(err, "add progress photo")
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Check{}, apperr.Wrap(err, "commit progress check")
	}
	check := fromDB(row)
	check.Photos = photos
	return check, nil
}

func (r *Repository) Get(ctx context.Context, id, userID uuid.UUID) (Check, error) {
	row, err := r.q.GetCheck(ctx, progresschecksdb.GetCheckParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Check{}, apperr.ErrNotFound
	}
	if err != nil {
		return Check{}, apperr.Wrap(err, "get progress check")
	}
	checks, err := r.withPhotos(ctx, userID, []Check{fromDB(row)})
	if err != nil {
		return Check{}, err
	}
	return checks[0], nil
}

// List is the newest checks first, each with its photos.
func (r *Repository) List(ctx context.Context, userID uuid.UUID, limit int) ([]Check, error) {
	rows, err := r.q.ListChecks(ctx, progresschecksdb.ListChecksParams{UserID: userID, Limit: int32(limit)})
	if err != nil {
		return nil, apperr.Wrap(err, "list progress checks")
	}
	out := make([]Check, len(rows))
	for i, row := range rows {
		out[i] = fromDB(row)
	}
	return r.withPhotos(ctx, userID, out)
}

func (r *Repository) withPhotos(ctx context.Context, userID uuid.UUID, checks []Check) ([]Check, error) {
	if len(checks) == 0 {
		return checks, nil
	}
	ids := make([]uuid.UUID, len(checks))
	index := make(map[uuid.UUID]int, len(checks))
	for i, c := range checks {
		ids[i], index[c.ID] = c.ID, i
	}
	rows, err := r.q.ListCheckPhotos(ctx, progresschecksdb.ListCheckPhotosParams{UserID: userID, CheckIds: ids})
	if err != nil {
		return nil, apperr.Wrap(err, "list progress photos")
	}
	for _, row := range rows {
		i := index[row.CheckID]
		checks[i].Photos = append(checks[i].Photos, Photo{Pose: progresscheck.Pose(row.Pose), MediaID: row.MediaID})
	}
	for i := range checks {
		sortPhotos(checks[i].Photos)
	}
	return checks, nil
}

func (r *Repository) Delete(ctx context.Context, id, userID uuid.UUID) error {
	n, err := r.q.DeleteCheck(ctx, progresschecksdb.DeleteCheckParams{ID: id, UserID: userID})
	if err != nil {
		return apperr.Wrap(err, "delete progress check")
	}
	if n == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

// Schedule is the person's cadence; the zero Schedule when none was set.
func (r *Repository) Schedule(ctx context.Context, userID uuid.UUID) (Schedule, error) {
	row, err := r.q.GetSchedule(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Schedule{}, nil
	}
	if err != nil {
		return Schedule{}, apperr.Wrap(err, "get progress schedule")
	}
	return Schedule{Cadence: progresscheck.Cadence(row.Cadence), NextDueOn: row.NextDueOn.Time}, nil
}

func (r *Repository) SetSchedule(ctx context.Context, userID uuid.UUID, s Schedule) (Schedule, error) {
	row, err := r.q.UpsertSchedule(ctx, progresschecksdb.UpsertScheduleParams{
		UserID: userID, Cadence: string(s.Cadence), NextDueOn: date(s.NextDueOn),
	})
	if err != nil {
		return Schedule{}, apperr.Wrap(err, "set progress schedule")
	}
	return Schedule{Cadence: progresscheck.Cadence(row.Cadence), NextDueOn: row.NextDueOn.Time}, nil
}

func (r *Repository) ClearSchedule(ctx context.Context, userID uuid.UUID) error {
	if err := r.q.DeleteSchedule(ctx, userID); err != nil {
		return apperr.Wrap(err, "clear progress schedule")
	}
	return nil
}

func fromDB(row progresschecksdb.ProgressCheck) Check {
	return Check{
		ID: row.ID, MeasuredOn: row.MeasuredOn.Time,
		Measurements: progresscheck.Measurements{
			WeightKg: row.WeightKg, WaistCm: row.WaistCm, HipsCm: row.HipsCm,
			ChestCm: row.ChestCm, ThighCm: row.ThighCm, ArmCm: row.ArmCm,
		},
		WeightCarried: row.WeightCarried, Source: row.Source,
		AssistConfidence: row.AssistConfidence, AssistNotes: row.AssistNotes,
		CreatedAt: row.CreatedAt,
	}
}

// sortPhotos puts a check's photos in pose order: front, side, back.
func sortPhotos(photos []Photo) {
	rank := map[progresscheck.Pose]int{}
	for i, p := range progresscheck.Poses {
		rank[p] = i
	}
	for i := 1; i < len(photos); i++ {
		for j := i; j > 0 && rank[photos[j].Pose] < rank[photos[j-1].Pose]; j-- {
			photos[j], photos[j-1] = photos[j-1], photos[j]
		}
	}
}

func date(t time.Time) pgtype.Date { return pgtype.Date{Time: t, Valid: true} }

// FirstAndLatestWithPhotos are the two checks a comparison reads: the oldest
// and the newest with a front or side photo. ErrNotFound when there is none;
// the two are the same check when there is only one.
func (r *Repository) FirstAndLatestWithPhotos(ctx context.Context, userID uuid.UUID) (first, latest Check, err error) {
	oldest, err := r.q.OldestCheckWithPhotos(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Check{}, Check{}, apperr.ErrNotFound
	}
	if err != nil {
		return Check{}, Check{}, apperr.Wrap(err, "oldest progress check with photos")
	}
	newest, err := r.q.NewestCheckWithPhotos(ctx, userID)
	if err != nil {
		return Check{}, Check{}, apperr.Wrap(err, "newest progress check with photos")
	}
	checks, err := r.withPhotos(ctx, userID, []Check{fromDB(oldest), fromDB(newest)})
	if err != nil {
		return Check{}, Check{}, err
	}
	if checks[0].ID == checks[1].ID {
		// withPhotos indexes by id, so both copies point at one slot; the
		// photos landed on the second.
		checks[0] = checks[1]
	}
	return checks[0], checks[1], nil
}

// SaveComparison stores the person's comparison, replacing the last one.
func (r *Repository) SaveComparison(ctx context.Context, userID uuid.UUID, c Comparison) error {
	if err := r.q.UpsertComparison(ctx, progresschecksdb.UpsertComparisonParams{
		UserID: userID, FromCheckID: c.FromCheckID, ToCheckID: c.ToCheckID,
		Summary: c.Summary, Confidence: c.Confidence, Limits: c.Limits, ProviderLabel: c.Provider,
	}); err != nil {
		return apperr.Wrap(err, "save progress comparison")
	}
	return nil
}

// Comparison is the person's latest comparison; ErrNotFound when they have
// none, or when a check it compared was deleted.
func (r *Repository) Comparison(ctx context.Context, userID uuid.UUID) (Comparison, error) {
	row, err := r.q.GetComparison(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Comparison{}, apperr.ErrNotFound
	}
	if err != nil {
		return Comparison{}, apperr.Wrap(err, "get progress comparison")
	}
	return Comparison{
		FromCheckID: row.FromCheckID, ToCheckID: row.ToCheckID,
		FromOn: row.FromOn.Time, ToOn: row.ToOn.Time,
		Summary: row.Summary, Confidence: row.Confidence, Limits: row.Limits,
		Provider: row.ProviderLabel, CreatedAt: row.CreatedAt,
	}, nil
}
