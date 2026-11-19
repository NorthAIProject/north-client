package media

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NorthAIProject/north-client/internal/media/analysis"
	mediadb "github.com/NorthAIProject/north-client/internal/media/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

// Media is an uploaded file.
type Media struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	Kind         string
	MIMEType     string
	SizeBytes    int64
	StorageKey   string
	OriginalName string
	CreatedAt    time.Time
}

type PostgresRepository struct {
	pool *pgxpool.Pool
	q    *mediadb.Queries
}

var _ Repository = (*PostgresRepository)(nil)

func NewRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool, q: mediadb.New(pool)}
}

type NewMedia struct {
	UserID       uuid.UUID
	Kind         string
	MIMEType     string
	SizeBytes    int64
	StorageKey   string
	OriginalName string
}

func (r *PostgresRepository) CreateMedia(ctx context.Context, m NewMedia) (Media, error) {
	row, err := r.q.CreateMedia(ctx, mediadb.CreateMediaParams{
		UserID:       m.UserID,
		Kind:         m.Kind,
		MimeType:     m.MIMEType,
		SizeBytes:    m.SizeBytes,
		StorageKey:   m.StorageKey,
		OriginalName: m.OriginalName,
	})
	if err != nil {
		return Media{}, apperr.Wrap(err, "create media")
	}
	return mediaFromDB(row), nil
}

func (r *PostgresRepository) CountByKind(ctx context.Context, userID uuid.UUID, kind string) (int, error) {
	n, err := r.q.CountUserMediaByKind(ctx, mediadb.CountUserMediaByKindParams{
		UserID: userID,
		Kind:   kind,
	})
	if err != nil {
		return 0, apperr.Wrap(err, "count media")
	}
	return int(n), nil
}

func (r *PostgresRepository) LatestCreatedAt(ctx context.Context, userID uuid.UUID, kind string) (time.Time, bool, error) {
	at, err := r.q.LatestUserMediaCreatedAt(ctx, mediadb.LatestUserMediaCreatedAtParams{
		UserID: userID,
		Kind:   kind,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, apperr.Wrap(err, "latest media")
	}
	return at, true, nil
}

func (r *PostgresRepository) GetMedia(ctx context.Context, id, userID uuid.UUID) (Media, error) {
	row, err := r.q.GetMedia(ctx, mediadb.GetMediaParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Media{}, apperr.ErrNotFound
		}
		return Media{}, apperr.Wrap(err, "get media")
	}
	return mediaFromDB(row), nil
}

// DeleteMedia removes a record this person owns and returns its storage key.
func (r *PostgresRepository) DeleteMedia(ctx context.Context, id, userID uuid.UUID) (string, error) {
	key, err := r.q.DeleteMedia(ctx, mediadb.DeleteMediaParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", apperr.ErrNotFound
		}
		return "", apperr.Wrap(err, "delete media")
	}
	return key, nil
}

// LatestOfKinds is the person's newest media of one of kinds, and with name
// set, the newest whose original name matches it case-insensitively.
func (r *PostgresRepository) LatestOfKinds(ctx context.Context, userID uuid.UUID, kinds []string, name string) (Media, error) {
	row, err := r.q.LatestUserMediaOfKinds(ctx, mediadb.LatestUserMediaOfKindsParams{
		UserID: userID,
		Kinds:  kinds,
		Name:   name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Media{}, apperr.ErrNotFound
		}
		return Media{}, apperr.Wrap(err, "latest media of kinds")
	}
	return mediaFromDB(row), nil
}

// GetMediaByID is unscoped, for the worker, which acts for the system rather
// than a signed-in user. Handlers must use GetMedia.
func (r *PostgresRepository) GetMediaByID(ctx context.Context, id uuid.UUID) (Media, error) {
	row, err := r.q.GetMediaByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Media{}, apperr.ErrNotFound
		}
		return Media{}, apperr.Wrap(err, "get media by id")
	}
	return mediaFromDB(row), nil
}

// NewAnalysis is a form check about to be queued, with the stills it will be
// read from. No frames means the clip is read as video, where a provider can.
type NewAnalysis struct {
	MediaID         uuid.UUID
	UserID          uuid.UUID
	DurationSeconds float64
	ExerciseSlug    string
	Frames          []analysis.Frame
}

// StoredFrame is one still of a check and where its bytes are kept.
type StoredFrame struct {
	analysis.Frame
	StorageKey string
	MIMEType   string
}

// CreateAnalysis writes the check and its stills together, so a check is never
// left pointing at half its frames.
func (r *PostgresRepository) CreateAnalysis(ctx context.Context, in NewAnalysis) (analysis.Analysis, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return analysis.Analysis{}, apperr.Wrap(err, "begin create analysis")
	}
	defer func() { _ = tx.Rollback(ctx) }()

	q := r.q.WithTx(tx)
	row, err := q.CreateAnalysis(ctx, mediadb.CreateAnalysisParams{
		MediaID:         in.MediaID,
		UserID:          in.UserID,
		DurationSeconds: in.DurationSeconds,
		ExerciseSlug:    in.ExerciseSlug,
	})
	if err != nil {
		return analysis.Analysis{}, apperr.Wrap(err, "create analysis")
	}
	for _, f := range in.Frames {
		if err := q.AddAnalysisFrame(ctx, mediadb.AddAnalysisFrameParams{
			AnalysisID: row.ID,
			Idx:        int32(f.Index),
			MediaID:    f.MediaID,
			AtSeconds:  f.AtSeconds,
		}); err != nil {
			return analysis.Analysis{}, apperr.Wrap(err, "add analysis frame %d", f.Index)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return analysis.Analysis{}, apperr.Wrap(err, "commit create analysis")
	}
	return analysisFromDB(row), nil
}

func (r *PostgresRepository) ListFrames(ctx context.Context, analysisID uuid.UUID) ([]StoredFrame, error) {
	rows, err := r.q.ListAnalysisFrames(ctx, analysisID)
	if err != nil {
		return nil, apperr.Wrap(err, "list analysis frames")
	}
	out := make([]StoredFrame, 0, len(rows))
	for _, row := range rows {
		out = append(out, StoredFrame{
			Frame:      analysis.Frame{Index: int(row.Idx), MediaID: row.MediaID, AtSeconds: row.AtSeconds},
			StorageKey: row.StorageKey,
			MIMEType:   row.MimeType,
		})
	}
	return out, nil
}

func (r *PostgresRepository) GetAnalysis(ctx context.Context, id, userID uuid.UUID) (analysis.Analysis, error) {
	row, err := r.q.GetAnalysis(ctx, mediadb.GetAnalysisParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return analysis.Analysis{}, apperr.ErrNotFound
		}
		return analysis.Analysis{}, apperr.Wrap(err, "get analysis")
	}
	return analysisFromDB(row), nil
}

func (r *PostgresRepository) GetAnalysisByMedia(ctx context.Context, mediaID uuid.UUID) (analysis.Analysis, error) {
	row, err := r.q.GetAnalysisByMedia(ctx, mediaID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return analysis.Analysis{}, apperr.ErrNotFound
		}
		return analysis.Analysis{}, apperr.Wrap(err, "get analysis by media")
	}
	return analysisFromDB(row), nil
}

func (r *PostgresRepository) ListAnalyses(ctx context.Context, userID uuid.UUID, limit int) ([]analysis.Analysis, error) {
	rows, err := r.q.ListAnalyses(ctx, mediadb.ListAnalysesParams{UserID: userID, Limit: int32(limit)})
	if err != nil {
		return nil, apperr.Wrap(err, "list analyses")
	}

	out := make([]analysis.Analysis, 0, len(rows))
	for _, row := range rows {
		out = append(out, analysisFromDB(row))
	}
	return out, nil
}

func (r *PostgresRepository) StartAnalysis(ctx context.Context, id uuid.UUID) error {
	return apperr.Wrap(r.q.StartAnalysis(ctx, id), "start analysis")
}

func (r *PostgresRepository) CompleteAnalysis(ctx context.Context, id uuid.UUID, result analysis.FormAnalysis, model, provider string) error {
	body, err := json.Marshal(result)
	if err != nil {
		return apperr.Wrap(err, "encode analysis")
	}

	return apperr.Wrap(r.q.CompleteAnalysis(ctx, mediadb.CompleteAnalysisParams{
		ID:       id,
		Analysis: body,
		Model:    model,
		Provider: provider,
	}), "complete analysis")
}

func (r *PostgresRepository) FailAnalysis(ctx context.Context, id uuid.UUID, reason string) error {
	return apperr.Wrap(r.q.FailAnalysis(ctx, mediadb.FailAnalysisParams{ID: id, Error: reason}), "fail analysis")
}

func mediaFromDB(row mediadb.Medium) Media {
	return Media{
		ID:           row.ID,
		UserID:       row.UserID,
		Kind:         row.Kind,
		MIMEType:     row.MimeType,
		SizeBytes:    row.SizeBytes,
		StorageKey:   row.StorageKey,
		OriginalName: row.OriginalName,
		CreatedAt:    row.CreatedAt,
	}
}

func analysisFromDB(row mediadb.FormAnalysis) analysis.Analysis {
	a := analysis.Analysis{
		ID:        row.ID,
		MediaID:   row.MediaID,
		UserID:    row.UserID,
		Status:    row.Status,
		Error:     row.Error,
		Model:     row.Model,
		Provider:  row.Provider,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,

		DurationSeconds: row.DurationSeconds,
		ExerciseSlug:    row.ExerciseSlug,
	}

	if len(row.Analysis) > 0 {
		var result analysis.FormAnalysis
		if json.Unmarshal(row.Analysis, &result) == nil {
			a.Result = &result
		}
	}

	return a
}
