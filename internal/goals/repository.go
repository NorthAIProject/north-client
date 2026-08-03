package goals

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	goalsdb "github.com/NorthAIProject/north-client/internal/goals/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *goalsdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: goalsdb.New(pool)}
}

// NewGoal is a goal to create.
type NewGoal struct {
	Title      string
	Motivation string
	Success    string
	Category   string
	TargetDate time.Time
}

func (r *Repository) Create(ctx context.Context, userID uuid.UUID, g NewGoal) (Goal, error) {
	row, err := r.q.CreateGoal(ctx, goalsdb.CreateGoalParams{
		UserID:     userID,
		Title:      g.Title,
		Motivation: g.Motivation,
		Success:    g.Success,
		Category:   g.Category,
		TargetDate: toDate(g.TargetDate),
	})
	if err != nil {
		return Goal{}, apperr.Wrap(err, "create goal")
	}
	return fromDB(row), nil
}

func (r *Repository) Get(ctx context.Context, id, userID uuid.UUID) (Goal, error) {
	row, err := r.q.GetGoal(ctx, goalsdb.GetGoalParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Goal{}, apperr.ErrNotFound
		}
		return Goal{}, apperr.Wrap(err, "get goal")
	}
	return fromDB(row), nil
}

func (r *Repository) List(ctx context.Context, userID uuid.UUID, limit int) ([]Goal, error) {
	rows, err := r.q.ListGoals(ctx, goalsdb.ListGoalsParams{UserID: userID, Limit: int32(limit)})
	if err != nil {
		return nil, apperr.Wrap(err, "list goals")
	}
	return r.withLatestUpdates(ctx, userID, goalsFromDB(rows))
}

func (r *Repository) ListActive(ctx context.Context, userID uuid.UUID, limit int) ([]Goal, error) {
	rows, err := r.q.ListActiveGoals(ctx, goalsdb.ListActiveGoalsParams{UserID: userID, Limit: int32(limit)})
	if err != nil {
		return nil, apperr.Wrap(err, "list active goals")
	}
	return r.withLatestUpdates(ctx, userID, goalsFromDB(rows))
}

// withLatestUpdates attaches each goal's most recent note.
//
// One query for every goal rather than one per goal: the coach loads this on
// every message, and N+1 there is a query per goal per message forever.
func (r *Repository) withLatestUpdates(ctx context.Context, userID uuid.UUID, list []Goal) ([]Goal, error) {
	if len(list) == 0 {
		return list, nil
	}

	rows, err := r.q.LatestGoalUpdates(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "latest goal updates")
	}

	latest := make(map[uuid.UUID]Update, len(rows))
	for _, row := range rows {
		latest[row.GoalID] = updateFromDB(row)
	}

	for i := range list {
		if update, ok := latest[list[i].ID]; ok {
			list[i].LatestUpdate = &update
		}
	}

	return list, nil
}

func (r *Repository) Update(ctx context.Context, id, userID uuid.UUID, g NewGoal) (Goal, error) {
	row, err := r.q.UpdateGoal(ctx, goalsdb.UpdateGoalParams{
		ID:         id,
		UserID:     userID,
		Title:      g.Title,
		Motivation: g.Motivation,
		Success:    g.Success,
		Category:   g.Category,
		TargetDate: toDate(g.TargetDate),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Goal{}, apperr.ErrNotFound
		}
		return Goal{}, apperr.Wrap(err, "update goal")
	}
	return fromDB(row), nil
}

func (r *Repository) SetStatus(ctx context.Context, id, userID uuid.UUID, status string) (Goal, error) {
	row, err := r.q.SetGoalStatus(ctx, goalsdb.SetGoalStatusParams{
		ID:     id,
		UserID: userID,
		Status: status,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Goal{}, apperr.ErrNotFound
		}
		return Goal{}, apperr.Wrap(err, "set goal status")
	}
	return fromDB(row), nil
}

func (r *Repository) Delete(ctx context.Context, id, userID uuid.UUID) error {
	return apperr.Wrap(r.q.DeleteGoal(ctx, goalsdb.DeleteGoalParams{ID: id, UserID: userID}), "delete goal")
}

func (r *Repository) AddUpdate(ctx context.Context, goalID, userID uuid.UUID, note string, progress *int) (Update, error) {
	var p *int16
	if progress != nil {
		v := int16(*progress)
		p = &v
	}

	row, err := r.q.AddGoalUpdate(ctx, goalsdb.AddGoalUpdateParams{
		GoalID:   goalID,
		UserID:   userID,
		Note:     note,
		Progress: p,
	})
	if err != nil {
		return Update{}, apperr.Wrap(err, "add goal update")
	}
	return updateFromDB(row), nil
}

func (r *Repository) Updates(ctx context.Context, goalID uuid.UUID, limit int) ([]Update, error) {
	rows, err := r.q.ListGoalUpdates(ctx, goalsdb.ListGoalUpdatesParams{GoalID: goalID, Limit: int32(limit)})
	if err != nil {
		return nil, apperr.Wrap(err, "list goal updates")
	}

	out := make([]Update, 0, len(rows))
	for _, row := range rows {
		out = append(out, updateFromDB(row))
	}
	return out, nil
}

func goalsFromDB(rows []goalsdb.Goal) []Goal {
	out := make([]Goal, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDB(row))
	}
	return out
}

func fromDB(row goalsdb.Goal) Goal {
	g := Goal{
		ID:         row.ID,
		UserID:     row.UserID,
		Title:      row.Title,
		Motivation: row.Motivation,
		Success:    row.Success,
		Category:   row.Category,
		Status:     row.Status,
		CreatedAt:  row.CreatedAt,
		UpdatedAt:  row.UpdatedAt,
		ClosedAt:   row.ClosedAt,
	}
	if row.TargetDate.Valid {
		g.TargetDate = row.TargetDate.Time
	}
	return g
}

func updateFromDB(row goalsdb.GoalUpdate) Update {
	u := Update{
		ID:        row.ID,
		GoalID:    row.GoalID,
		Note:      row.Note,
		CreatedAt: row.CreatedAt,
	}
	if row.Progress != nil {
		p := int(*row.Progress)
		u.Progress = &p
	}
	return u
}

// toDate converts a zero time to SQL NULL, which is how "no deadline" is
// stored. A zero time written as a date would read as the year 1.
func toDate(t time.Time) pgtype.Date {
	if t.IsZero() {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: t, Valid: true}
}
