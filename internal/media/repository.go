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

type Repository struct {
	q *mediadb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: mediadb.New(pool)}
}

type NewMedia struct {
	UserID       uuid.UUID
	Kind         string
	MIMEType     string
	SizeBytes    int64
	StorageKey   string
	OriginalName string
}

func (r *Repository) CreateMedia(ctx context.Context, m NewMedia) (Media, error) {
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

func (r *Repository) CountByKind(ctx context.Context, userID uuid.UUID, kind string) (int, error) {
	n, err := r.q.CountUserMediaByKind(ctx, mediadb.CountUserMediaByKindParams{
		UserID: userID,
		Kind:   kind,
	})
	if err != nil {
		return 0, apperr.Wrap(err, "count media")
	}
	return int(n), nil
}

func (r *Repository) LatestCreatedAt(ctx context.Context, userID uuid.UUID, kind string) (time.Time, bool, error) {
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

func (r *Repository) GetMedia(ctx context.Context, id, userID uuid.UUID) (Media, error) {
	row, err := r.q.GetMedia(ctx, mediadb.GetMediaParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Media{}, apperr.ErrNotFound
		}
		return Media{}, apperr.Wrap(err, "get media")
	}
	return mediaFromDB(row), nil
}

// GetMediaByID is unscoped, for the worker, which acts for the system rather
// than a signed-in user. Handlers must use GetMedia.
func (r *Repository) GetMediaByID(ctx context.Context, id uuid.UUID) (Media, error) {
	row, err := r.q.GetMediaByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Media{}, apperr.ErrNotFound
		}
		return Media{}, apperr.Wrap(err, "get media by id")
	}
	return mediaFromDB(row), nil
}

func (r *Repository) CreateAnalysis(ctx context.Context, mediaID, userID uuid.UUID) (analysis.Analysis, error) {
	row, err := r.q.CreateAnalysis(ctx, mediadb.CreateAnalysisParams{MediaID: mediaID, UserID: userID})
	if err != nil {
		return analysis.Analysis{}, apperr.Wrap(err, "create analysis")
	}
	return analysisFromDB(row), nil
}

func (r *Repository) GetAnalysis(ctx context.Context, id, userID uuid.UUID) (analysis.Analysis, error) {
	row, err := r.q.GetAnalysis(ctx, mediadb.GetAnalysisParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return analysis.Analysis{}, apperr.ErrNotFound
		}
		return analysis.Analysis{}, apperr.Wrap(err, "get analysis")
	}
	return analysisFromDB(row), nil
}

func (r *Repository) GetAnalysisByMedia(ctx context.Context, mediaID uuid.UUID) (analysis.Analysis, error) {
	row, err := r.q.GetAnalysisByMedia(ctx, mediaID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return analysis.Analysis{}, apperr.ErrNotFound
		}
		return analysis.Analysis{}, apperr.Wrap(err, "get analysis by media")
	}
	return analysisFromDB(row), nil
}

func (r *Repository) ListAnalyses(ctx context.Context, userID uuid.UUID, limit int) ([]analysis.Analysis, error) {
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

func (r *Repository) StartAnalysis(ctx context.Context, id uuid.UUID) error {
	return apperr.Wrap(r.q.StartAnalysis(ctx, id), "start analysis")
}

func (r *Repository) CompleteAnalysis(ctx context.Context, id uuid.UUID, result analysis.FormAnalysis, model, provider string) error {
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

func (r *Repository) FailAnalysis(ctx context.Context, id uuid.UUID, reason string) error {
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
	}

	if len(row.Analysis) > 0 {
		var result analysis.FormAnalysis
		if json.Unmarshal(row.Analysis, &result) == nil {
			a.Result = &result
		}
	}

	return a
}
