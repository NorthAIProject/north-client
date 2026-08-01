package workouts

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	workoutsdb "github.com/NorthAIProject/north-client/internal/workouts/db"
)

// StoredIntake is a persisted intake.
type StoredIntake struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Intake    Intake
	CreatedAt time.Time
}

// StoredPlan is a persisted, already-validated plan.
type StoredPlan struct {
	ID       uuid.UUID
	UserID   uuid.UUID
	IntakeID uuid.UUID
	Plan     Plan
	Model    string
	Provider string

	CreatedAt time.Time
}

type Repository struct {
	q *workoutsdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: workoutsdb.New(pool)}
}

func (r *Repository) CreateIntake(ctx context.Context, userID uuid.UUID, in Intake) (StoredIntake, error) {
	row, err := r.q.CreateIntake(ctx, workoutsdb.CreateIntakeParams{
		UserID:         userID,
		Goal:           in.Goal,
		Experience:     in.Experience,
		DaysPerWeek:    int16(in.DaysPerWeek),
		SessionMinutes: int16(in.SessionMinutes),
		Equipment:      in.Equipment,
		Limitations:    in.Limitations,
	})
	if err != nil {
		return StoredIntake{}, apperr.Wrap(err, "create intake")
	}
	return intakeFromDB(row), nil
}

func (r *Repository) LatestIntake(ctx context.Context, userID uuid.UUID) (StoredIntake, error) {
	row, err := r.q.LatestIntake(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StoredIntake{}, apperr.ErrNotFound
		}
		return StoredIntake{}, apperr.Wrap(err, "latest intake")
	}
	return intakeFromDB(row), nil
}

func (r *Repository) CreatePlan(ctx context.Context, p StoredPlan) (StoredPlan, error) {
	body, err := json.Marshal(p.Plan)
	if err != nil {
		return StoredPlan{}, apperr.Wrap(err, "encode plan")
	}

	row, err := r.q.CreatePlan(ctx, workoutsdb.CreatePlanParams{
		UserID:   p.UserID,
		IntakeID: p.IntakeID,
		Name:     p.Plan.Name,
		Plan:     body,
		Model:    p.Model,
		Provider: p.Provider,
	})
	if err != nil {
		return StoredPlan{}, apperr.Wrap(err, "create plan")
	}
	return planFromDB(row)
}

func (r *Repository) GetPlan(ctx context.Context, id, userID uuid.UUID) (StoredPlan, error) {
	row, err := r.q.GetPlan(ctx, workoutsdb.GetPlanParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StoredPlan{}, apperr.ErrNotFound
		}
		return StoredPlan{}, apperr.Wrap(err, "get plan")
	}
	return planFromDB(row)
}

func (r *Repository) LatestPlan(ctx context.Context, userID uuid.UUID) (StoredPlan, error) {
	row, err := r.q.LatestPlan(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StoredPlan{}, apperr.ErrNotFound
		}
		return StoredPlan{}, apperr.Wrap(err, "latest plan")
	}
	return planFromDB(row)
}

func (r *Repository) ListPlans(ctx context.Context, userID uuid.UUID, limit int) ([]StoredPlan, error) {
	rows, err := r.q.ListPlans(ctx, workoutsdb.ListPlansParams{UserID: userID, Limit: int32(limit)})
	if err != nil {
		return nil, apperr.Wrap(err, "list plans")
	}

	out := make([]StoredPlan, 0, len(rows))
	for _, row := range rows {
		plan, err := planFromDB(row)
		if err != nil {
			return nil, err
		}
		out = append(out, plan)
	}
	return out, nil
}

func intakeFromDB(row workoutsdb.WorkoutIntake) StoredIntake {
	return StoredIntake{
		ID:        row.ID,
		UserID:    row.UserID,
		CreatedAt: row.CreatedAt,
		Intake: Intake{
			Goal:           row.Goal,
			Experience:     row.Experience,
			DaysPerWeek:    int(row.DaysPerWeek),
			SessionMinutes: int(row.SessionMinutes),
			Equipment:      row.Equipment,
			Limitations:    row.Limitations,
		},
	}
}

func planFromDB(row workoutsdb.WorkoutPlan) (StoredPlan, error) {
	var plan Plan
	// Unlike message metadata, a plan that will not decode is not decoration:
	// rendering an empty plan would tell the user they have no training to do.
	if err := json.Unmarshal(row.Plan, &plan); err != nil {
		return StoredPlan{}, apperr.Wrap(err, "decode stored plan %s", row.ID)
	}

	return StoredPlan{
		ID:        row.ID,
		UserID:    row.UserID,
		IntakeID:  row.IntakeID,
		Plan:      plan,
		Model:     row.Model,
		Provider:  row.Provider,
		CreatedAt: row.CreatedAt,
	}, nil
}
