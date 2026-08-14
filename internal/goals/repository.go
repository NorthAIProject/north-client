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
	return r.withProgress(ctx, userID, goalsFromDB(rows))
}

func (r *Repository) ListActive(ctx context.Context, userID uuid.UUID, limit int) ([]Goal, error) {
	rows, err := r.q.ListActiveGoals(ctx, goalsdb.ListActiveGoalsParams{UserID: userID, Limit: int32(limit)})
	if err != nil {
		return nil, apperr.Wrap(err, "list active goals")
	}
	return r.withProgress(ctx, userID, goalsFromDB(rows))
}

// withProgress attaches each goal's most recent note and its milestone counts.
//
// Two queries for the whole list rather than one per goal: the coach loads
// this on every message, and N+1 there is a query per goal per message forever.
func (r *Repository) withProgress(ctx context.Context, userID uuid.UUID, list []Goal) ([]Goal, error) {
	list, err := r.withLatestUpdates(ctx, userID, list)
	if err != nil {
		return nil, err
	}
	return r.withMilestoneCounts(ctx, userID, list)
}

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

func (r *Repository) withMilestoneCounts(ctx context.Context, userID uuid.UUID, list []Goal) ([]Goal, error) {
	if len(list) == 0 {
		return list, nil
	}

	rows, err := r.q.MilestoneCounts(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "milestone counts")
	}

	type counts struct{ total, done int }
	byGoal := make(map[uuid.UUID]counts, len(rows))
	for _, row := range rows {
		byGoal[row.GoalID] = counts{total: int(row.Total), done: int(row.Completed)}
	}

	for i := range list {
		if c, ok := byGoal[list[i].ID]; ok {
			list[i].MilestoneTotal = c.total
			list[i].MilestoneDone = c.done
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

func (r *Repository) Updates(ctx context.Context, goalID, userID uuid.UUID, limit int) ([]Update, error) {
	rows, err := r.q.ListGoalUpdates(ctx, goalsdb.ListGoalUpdatesParams{
		GoalID: goalID,
		UserID: userID,
		Limit:  int32(limit),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list goal updates")
	}

	out := make([]Update, 0, len(rows))
	for _, row := range rows {
		out = append(out, updateFromDB(row))
	}
	return out, nil
}

// TimelineUpdate is a goal note carrying the title of the goal it belongs to,
// so a cross-domain feed can name it without a second query per row.
type TimelineUpdate struct {
	Update
	GoalTitle string
}

// UpdatesBetween returns every note across every goal in the half-open window
// [since, until), newest first.
func (r *Repository) UpdatesBetween(ctx context.Context, userID uuid.UUID, since, until time.Time) ([]TimelineUpdate, error) {
	rows, err := r.q.ListGoalUpdatesBetween(ctx, goalsdb.ListGoalUpdatesBetweenParams{
		UserID:      userID,
		CreatedAt:   since,
		CreatedAt_2: until,
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list goal updates between")
	}

	out := make([]TimelineUpdate, 0, len(rows))
	for _, row := range rows {
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
		out = append(out, TimelineUpdate{Update: u, GoalTitle: row.GoalTitle})
	}
	return out, nil
}

// CreatedBetween returns the goals opened in the half-open window
// [since, until). Deliberately without progress, latest-update, or milestone
// enrichment: a timeline entry needs a title and a date, and the enrichment
// costs three extra queries.
func (r *Repository) CreatedBetween(ctx context.Context, userID uuid.UUID, since, until time.Time) ([]Goal, error) {
	rows, err := r.q.ListGoalsCreatedBetween(ctx, goalsdb.ListGoalsCreatedBetweenParams{
		UserID:      userID,
		CreatedAt:   since,
		CreatedAt_2: until,
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list goals created between")
	}

	out := make([]Goal, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromDB(row))
	}
	return out, nil
}

func (r *Repository) AddMilestone(ctx context.Context, goalID, userID uuid.UUID, title string, targetDate time.Time) (Milestone, error) {
	row, err := r.q.CreateMilestone(ctx, goalsdb.CreateMilestoneParams{
		GoalID:     goalID,
		UserID:     userID,
		Title:      title,
		TargetDate: toDate(targetDate),
	})
	if err != nil {
		return Milestone{}, apperr.Wrap(err, "create milestone")
	}
	return milestoneFromDB(row), nil
}

func (r *Repository) GetMilestone(ctx context.Context, id, userID uuid.UUID) (Milestone, error) {
	row, err := r.q.GetMilestone(ctx, goalsdb.GetMilestoneParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Milestone{}, apperr.ErrNotFound
		}
		return Milestone{}, apperr.Wrap(err, "get milestone")
	}
	return milestoneFromDB(row), nil
}

func (r *Repository) UpdateMilestone(ctx context.Context, id, userID uuid.UUID, title string, targetDate time.Time) (Milestone, error) {
	row, err := r.q.UpdateMilestone(ctx, goalsdb.UpdateMilestoneParams{
		ID:         id,
		UserID:     userID,
		Title:      title,
		TargetDate: toDate(targetDate),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Milestone{}, apperr.ErrNotFound
		}
		return Milestone{}, apperr.Wrap(err, "update milestone")
	}
	return milestoneFromDB(row), nil
}

func (r *Repository) SetMilestoneStatus(ctx context.Context, id, userID uuid.UUID, status string) (Milestone, error) {
	row, err := r.q.SetMilestoneStatus(ctx, goalsdb.SetMilestoneStatusParams{
		ID:     id,
		UserID: userID,
		Status: status,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Milestone{}, apperr.ErrNotFound
		}
		return Milestone{}, apperr.Wrap(err, "set milestone status")
	}
	return milestoneFromDB(row), nil
}

func (r *Repository) DeleteMilestone(ctx context.Context, id, userID uuid.UUID) error {
	return apperr.Wrap(r.q.DeleteMilestone(ctx, goalsdb.DeleteMilestoneParams{ID: id, UserID: userID}), "delete milestone")
}

func (r *Repository) Milestones(ctx context.Context, goalID, userID uuid.UUID) ([]Milestone, error) {
	rows, err := r.q.ListMilestones(ctx, goalsdb.ListMilestonesParams{GoalID: goalID, UserID: userID})
	if err != nil {
		return nil, apperr.Wrap(err, "list milestones")
	}

	out := make([]Milestone, 0, len(rows))
	for _, row := range rows {
		out = append(out, milestoneFromDB(row))
	}
	return out, nil
}

func (r *Repository) CountOverdueMilestones(ctx context.Context, userID uuid.UUID) (int, error) {
	n, err := r.q.CountOverdueMilestones(ctx, userID)
	if err != nil {
		return 0, apperr.Wrap(err, "count overdue milestones")
	}
	return int(n), nil
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

func milestoneFromDB(row goalsdb.GoalMilestone) Milestone {
	m := Milestone{
		ID:          row.ID,
		GoalID:      row.GoalID,
		UserID:      row.UserID,
		Title:       row.Title,
		Status:      row.Status,
		Position:    int(row.Position),
		CompletedAt: row.CompletedAt,
		CreatedAt:   row.CreatedAt,
		UpdatedAt:   row.UpdatedAt,
	}
	if row.TargetDate.Valid {
		m.TargetDate = row.TargetDate.Time
	}
	return m
}

// toDate converts a zero time to SQL NULL, which is how "no deadline" is
// stored. A zero time written as a date would read as the year 1.
func toDate(t time.Time) pgtype.Date {
	if t.IsZero() {
		return pgtype.Date{}
	}
	return pgtype.Date{Time: t, Valid: true}
}
