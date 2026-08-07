package exercises

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	exercisesdb "github.com/NorthAIProject/north-client/internal/exercises/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *exercisesdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: exercisesdb.New(pool)}
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (Exercise, error) {
	row, err := r.q.GetExercise(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Exercise{}, apperr.ErrNotFound
		}
		return Exercise{}, apperr.Wrap(err, "get exercise")
	}
	return fromDB(row), nil
}

func (r *Repository) GetBySlug(ctx context.Context, slug string) (Exercise, error) {
	row, err := r.q.GetExerciseBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Exercise{}, apperr.ErrNotFound
		}
		return Exercise{}, apperr.Wrap(err, "get exercise by slug")
	}
	return fromDB(row), nil
}

// BySlugs returns the exercises for the given slugs, keyed by slug.
//
// A map rather than a slice because the caller resolving a generated plan
// looks up one name at a time, and missing slugs are expected: the model is
// allowed to improvise past the catalog.
func (r *Repository) BySlugs(ctx context.Context, slugs []string) (map[string]Exercise, error) {
	if len(slugs) == 0 {
		return map[string]Exercise{}, nil
	}

	rows, err := r.q.ListExercisesBySlugs(ctx, slugs)
	if err != nil {
		return nil, apperr.Wrap(err, "list exercises by slugs")
	}

	found := make(map[string]Exercise, len(rows))
	for _, row := range rows {
		found[row.Slug] = fromDB(row)
	}
	return found, nil
}

func (r *Repository) Search(ctx context.Context, f Filter) ([]Exercise, error) {
	rows, err := r.q.SearchExercises(ctx, exercisesdb.SearchExercisesParams{
		Query:       f.Query,
		Muscle:      f.Muscle,
		Category:    f.Category,
		Equipment:   f.Equipment,
		ResultLimit: int32(f.Limit),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "search exercises")
	}
	return fromDBRows(rows), nil
}

func (r *Repository) Count(ctx context.Context, f Filter) (int, error) {
	total, err := r.q.CountExercises(ctx, exercisesdb.CountExercisesParams{
		Query:     f.Query,
		Muscle:    f.Muscle,
		Category:  f.Category,
		Equipment: f.Equipment,
	})
	if err != nil {
		return 0, apperr.Wrap(err, "count exercises")
	}
	return int(total), nil
}

func (r *Repository) ForEquipment(ctx context.Context, equipment []string, limit int) ([]Exercise, error) {
	if len(equipment) == 0 {
		return nil, nil
	}

	rows, err := r.q.ListExercisesForEquipment(ctx, exercisesdb.ListExercisesForEquipmentParams{
		Equipment:   equipment,
		ResultLimit: int32(limit),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list exercises for equipment")
	}
	return fromDBRows(rows), nil
}

func fromDBRows(rows []exercisesdb.Exercise) []Exercise {
	out := make([]Exercise, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDB(row))
	}
	return out
}

func fromDB(row exercisesdb.Exercise) Exercise {
	return Exercise{
		ID:           row.ID,
		Slug:         row.Slug,
		Name:         row.Name,
		Category:     row.Category,
		Equipment:    row.Equipment,
		Difficulty:   row.Difficulty,
		Instructions: row.Instructions,
		VideoURL:     row.VideoUrl,
		Primary:      row.PrimaryMuscles,
		Secondary:    row.SecondaryMuscles,
	}
}
