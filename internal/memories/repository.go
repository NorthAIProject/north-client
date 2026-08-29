package memories

import (
	"context"
	"errors"
	"strings"

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
	Excluded             bool
	Source               string
	SourceConversationID *uuid.UUID
	Confidence           *float64

	// SupersedesID is the fact this row proposes to replace, or nil.
	SupersedesID *uuid.UUID
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
		Excluded:             m.Excluded,
		Source:               m.Source,
		SourceConversationID: m.SourceConversationID,
		Confidence:           conf,
		SupersedesID:         m.SupersedesID,
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

// Retrieved is a memory as it came back from a ranked search.
//
// Snippet and Rank are artifacts of one retrieval, not properties of the fact,
// so they live here rather than on Memory: a fact does not have a relevance
// score, it has one relative to a question.
type Retrieved struct {
	Memory

	// Snippet is the stored content with matching terms bracketed, or empty
	// for a fact that was included without matching — a pinned one.
	Snippet string

	// Rank is 0..1, comparable across tables. Zero for a pinned non-match.
	Rank float64
}

// SearchForContext returns approved memories ranked against query.
//
// Pinned facts come back whether or not they match; see the query for why.
func (r *Repository) SearchForContext(ctx context.Context, userID uuid.UUID, query string, limit int) ([]Retrieved, error) {
	rows, err := r.q.SearchApprovedForContext(ctx, memoriesdb.SearchApprovedForContextParams{
		UserID:      userID,
		Query:       query,
		ResultLimit: int32(limit),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "search memories for context")
	}

	out := make([]Retrieved, 0, len(rows))
	for _, row := range rows {
		snippet := row.Snippet
		// ts_headline on a non-matching row returns the opening words of the
		// content with nothing marked, which reads as a truncated fact rather
		// than as an excerpt. A pinned fact that did not match has no excerpt.
		if !strings.Contains(snippet, "[") {
			snippet = ""
		}
		out = append(out, Retrieved{
			Memory: Memory{
				ID:        row.ID,
				UserID:    userID,
				Category:  row.Category,
				Content:   row.Content,
				Status:    StatusApproved,
				Pinned:    row.Pinned,
				Excluded:  false,
				UpdatedAt: row.UpdatedAt,
			},
			Snippet: snippet,
			Rank:    row.Rank,
		})
	}
	return out, nil
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

func (r *Repository) SetExcluded(ctx context.Context, id, userID uuid.UUID, excluded bool) (Memory, error) {
	row, err := r.q.SetMemoryExcluded(ctx, memoriesdb.SetMemoryExcludedParams{
		ID:       id,
		UserID:   userID,
		Excluded: excluded,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Memory{}, apperr.ErrNotFound
		}
		return Memory{}, apperr.Wrap(err, "set memory excluded")
	}
	return fromDB(row), nil
}

func (r *Repository) SoftDelete(ctx context.Context, id, userID uuid.UUID) error {
	n, err := r.q.SoftDeleteMemory(ctx, memoriesdb.SoftDeleteMemoryParams{
		ID:     id,
		UserID: userID,
	})
	if err != nil {
		return apperr.Wrap(err, "soft delete memory")
	}
	if n == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

func (r *Repository) CountPending(ctx context.Context, userID uuid.UUID) (int, error) {
	n, err := r.q.CountPendingMemories(ctx, userID)
	if err != nil {
		return 0, apperr.Wrap(err, "count pending memories")
	}
	return int(n), nil
}

func (r *Repository) ListPendingForConversation(ctx context.Context, userID, conversationID uuid.UUID) ([]Memory, error) {
	rows, err := r.q.ListPendingForConversation(ctx, memoriesdb.ListPendingForConversationParams{
		UserID:               userID,
		SourceConversationID: &conversationID,
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list pending memories for conversation")
	}
	return fromDBList(rows), nil
}

// Supersede retires one fact, at the moment its replacement was approved.
//
// Reports false when nothing was retired, which is a normal outcome rather than
// an error: the target may have been deleted, already retired by an earlier
// approval, or pinned. Pinned is the interesting one — the query refuses it on
// purpose, because a pinned fact is one the person said always matters and a
// model's suggestion is not enough to overrule that.
func (r *Repository) Supersede(ctx context.Context, id, userID uuid.UUID) (Memory, bool, error) {
	row, err := r.q.SupersedeMemory(ctx, memoriesdb.SupersedeMemoryParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Memory{}, false, nil
	}
	if err != nil {
		return Memory{}, false, apperr.Wrap(err, "supersede memory")
	}
	return fromDB(row), true, nil
}

// CurrentFact is one believed fact, as offered to an extraction so it can say
// which of them a new fact replaces.
type CurrentFact struct {
	ID       uuid.UUID
	Category string
	Content  string
	Pinned   bool
}

// ListCurrentForSupersession returns the facts an extraction may propose
// replacing, oldest-updated last within each category.
func (r *Repository) ListCurrentForSupersession(ctx context.Context, userID uuid.UUID, limit int) ([]CurrentFact, error) {
	rows, err := r.q.ListCurrentForSupersession(ctx, memoriesdb.ListCurrentForSupersessionParams{
		UserID: userID,
		Limit:  int32(limit),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list current facts")
	}

	out := make([]CurrentFact, 0, len(rows))
	for _, row := range rows {
		out = append(out, CurrentFact{
			ID:       row.ID,
			Category: row.Category,
			Content:  row.Content,
			Pinned:   row.Pinned,
		})
	}
	return out, nil
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
		Excluded:             row.Excluded,
		Source:               row.Source,
		SourceConversationID: row.SourceConversationID,
		ValidTo:              row.ValidTo,
		SupersedesID:         row.SupersedesID,
		CreatedAt:            row.CreatedAt,
		UpdatedAt:            row.UpdatedAt,
	}
	if row.Confidence != nil {
		c := float64(*row.Confidence)
		m.Confidence = &c
	}
	return m
}
