package memories

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	memoriesdb "github.com/NorthAIProject/north-client/internal/memories/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *memoriesdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: memoriesdb.New(pool)}
}

// NewMemory is a row to insert.
type NewMemory struct {
	Category             string
	Content              string
	Status               string
	Pinned               bool
	Source               string
	SourceConversationID *uuid.UUID
	Confidence           *float64
}

func (r *Repository) Create(ctx context.Context, userID uuid.UUID, m NewMemory) (Memory, error) {
	var conf *float32
	if m.Confidence != nil {
		v := float32(*m.Confidence)
		conf = &v
	}
	row, err := r.q.CreateMemory(ctx, memoriesdb.CreateMemoryParams{
		UserID:               userID,
		Category:             m.Category,
		Content:              m.Content,
		Status:               m.Status,
		Pinned:               m.Pinned,
		Source:               m.Source,
		SourceConversationID: m.SourceConversationID,
		Confidence:           conf,
	})
	if err != nil {
		return Memory{}, apperr.Wrap(err, "create memory")
	}
	return fromDB(row), nil
}

func (r *Repository) Get(ctx context.Context, id, userID uuid.UUID) (Memory, error) {
	row, err := r.q.GetMemory(ctx, memoriesdb.GetMemoryParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Memory{}, apperr.ErrNotFound
		}
		return Memory{}, apperr.Wrap(err, "get memory")
	}
	return fromDB(row), nil
}

func (r *Repository) List(ctx context.Context, userID uuid.UUID, limit int) ([]Memory, error) {
	rows, err := r.q.ListMemories(ctx, memoriesdb.ListMemoriesParams{
		UserID: userID,
		Limit:  int32(limit),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list memories")
	}
	return fromDBList(rows), nil
}

func (r *Repository) ListByStatus(ctx context.Context, userID uuid.UUID, status string, limit int) ([]Memory, error) {
	rows, err := r.q.ListMemoriesByStatus(ctx, memoriesdb.ListMemoriesByStatusParams{
		UserID: userID,
		Status: status,
		Limit:  int32(limit),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list memories by status")
	}
	return fromDBList(rows), nil
}

func (r *Repository) ForContext(ctx context.Context, userID uuid.UUID, limit int) ([]Memory, error) {
	rows, err := r.q.ListApprovedForContext(ctx, memoriesdb.ListApprovedForContextParams{
		UserID: userID,
		Limit:  int32(limit),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list memories for context")
	}
	return fromDBList(rows), nil
}

func (r *Repository) ExistingContents(ctx context.Context, userID uuid.UUID) (map[string]bool, error) {
	rows, err := r.q.ListActiveContents(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "list memory contents")
	}
	out := make(map[string]bool, len(rows))
	for _, c := range rows {
		if c != "" {
			out[c] = true
		}
	}
	return out, nil
}

func (r *Repository) Update(ctx context.Context, id, userID uuid.UUID, category, content string) (Memory, error) {
	row, err := r.q.UpdateMemory(ctx, memoriesdb.UpdateMemoryParams{
		ID:       id,
		UserID:   userID,
		Category: category,
		Content:  content,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Memory{}, apperr.ErrNotFound
		}
		return Memory{}, apperr.Wrap(err, "update memory")
	}
	return fromDB(row), nil
}

func (r *Repository) SetStatus(ctx context.Context, id, userID uuid.UUID, status string) (Memory, error) {
	row, err := r.q.SetMemoryStatus(ctx, memoriesdb.SetMemoryStatusParams{
		ID:     id,
		UserID: userID,
		Status: status,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Memory{}, apperr.ErrNotFound
		}
		return Memory{}, apperr.Wrap(err, "set memory status")
	}
	return fromDB(row), nil
}

func (r *Repository) SetPinned(ctx context.Context, id, userID uuid.UUID, pinned bool) (Memory, error) {
	row, err := r.q.SetMemoryPinned(ctx, memoriesdb.SetMemoryPinnedParams{
		ID:     id,
		UserID: userID,
		Pinned: pinned,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Memory{}, apperr.ErrNotFound
		}
		return Memory{}, apperr.Wrap(err, "set memory pinned")
	}
	return fromDB(row), nil
}

func (r *Repository) SoftDelete(ctx context.Context, id, userID uuid.UUID) error {
	return apperr.Wrap(r.q.SoftDeleteMemory(ctx, memoriesdb.SoftDeleteMemoryParams{
		ID:     id,
		UserID: userID,
	}), "soft delete memory")
}

func (r *Repository) CountPending(ctx context.Context, userID uuid.UUID) (int, error) {
	n, err := r.q.CountPendingMemories(ctx, userID)
	if err != nil {
		return 0, apperr.Wrap(err, "count pending memories")
	}
	return int(n), nil
}

func fromDBList(rows []memoriesdb.UserMemory) []Memory {
	out := make([]Memory, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDB(row))
	}
	return out
}

func fromDB(row memoriesdb.UserMemory) Memory {
	m := Memory{
		ID:                   row.ID,
		UserID:               row.UserID,
		Category:             row.Category,
		Content:              row.Content,
		Status:               row.Status,
		Pinned:               row.Pinned,
		Source:               row.Source,
		SourceConversationID: row.SourceConversationID,
		CreatedAt:            row.CreatedAt,
		UpdatedAt:            row.UpdatedAt,
	}
	if row.Confidence != nil {
		c := float64(*row.Confidence)
		m.Confidence = &c
	}
	return m
}
