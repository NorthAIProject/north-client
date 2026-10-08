package workouts

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	workoutsdb "github.com/NorthAIProject/north-client/internal/workouts/db"
)

// StoredIntake is a persisted intake.
type StoredIntake struct {
	ID     uuid.UUID
	UserID uuid.UUID
	Intake Intake

	// Imported marks the placeholder row behind an imported plan. Its Intake
	// holds no answers and must not be read as if it did.
	Imported bool

	CreatedAt time.Time
}

// StoredPlan is a persisted, already-validated plan.
// A plan's provenance. Stored in workout_plans.source.
const (
	// SourceAI marks a plan exactly as the model produced it.
	SourceAI = "ai"

	// SourceEdited marks a plan a person changed. Editing inserts a new row
	// rather than updating one, so a chain of edits keeps every step — see
	// migrations/20260827190000.
	SourceEdited = "edited"

	// SourceImported marks a plan read from a file the person uploaded and
	// confirmed. No model generated it; Model and Provider say so.
	SourceImported = "imported"
)

type StoredPlan struct {
	ID       uuid.UUID
	UserID   uuid.UUID
	IntakeID uuid.UUID
	Plan     Plan
	Model    string
	Provider string

	// Source is SourceAI or SourceEdited. Model and Provider stay populated on
	// an edited plan — they record the generation it descends from, which is
	// still true after someone changes a lift; Source is what says a person
	// touched it.
	Source string

	// EditedFrom is the plan this one was edited from, nil for a generated one.
	EditedFrom *uuid.UUID

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

// CreateImportedIntake inserts the placeholder intake an imported plan hangs
// from. Only days_per_week means anything — it is the plan's own day count —
// and the rest are the column defaults or the smallest values the CHECKs
// allow. Nothing reads them: LatestIntake skips imported rows and
// PlanForDisplay does not validate against one.
func (r *Repository) CreateImportedIntake(ctx context.Context, userID uuid.UUID, daysPerWeek int) (StoredIntake, error) {
	row, err := r.q.CreateIntake(ctx, workoutsdb.CreateIntakeParams{
		UserID:         userID,
		Goal:           "",
		Experience:     "",
		DaysPerWeek:    int16(daysPerWeek),
		SessionMinutes: importedSessionMinutes,
		Equipment:      []string{},
		Imported:       true,
	})
	if err != nil {
		return StoredIntake{}, apperr.Wrap(err, "create imported intake")
	}
	return intakeFromDB(row), nil
}

// importedSessionMinutes satisfies session_minutes' CHECK on a row that has no
// session length. Unread; see CreateImportedIntake.
const importedSessionMinutes = 10

// GetIntake fetches the intake a plan was built from, so the plan page can say
// what it no longer satisfies. The plan's own intake rather than the newest:
// "this no longer matches what you asked for" is only true against the answers
// it was actually built from.
func (r *Repository) GetIntake(ctx context.Context, id, userID uuid.UUID) (StoredIntake, error) {
	row, err := r.q.GetIntake(ctx, workoutsdb.GetIntakeParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StoredIntake{}, apperr.ErrNotFound
		}
		return StoredIntake{}, apperr.Wrap(err, "get intake")
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

	// Callers that predate editing do not set Source, and a plan with an empty
	// provenance is worse than one that says where it came from.
	source := p.Source
	if source == "" {
		source = SourceAI
	}

	row, err := r.q.CreatePlan(ctx, workoutsdb.CreatePlanParams{
		UserID:   p.UserID,
		IntakeID: p.IntakeID,
		Name:     p.Plan.Name,
		Plan:     body,
		Model:    p.Model,
		Provider: p.Provider,

		Source:     source,
		EditedFrom: p.EditedFrom,
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

// LatestPlanForIntake is the newest version of one plan, for the edit guard.
func (r *Repository) LatestPlanForIntake(ctx context.Context, userID, intakeID uuid.UUID) (StoredPlan, error) {
	row, err := r.q.LatestPlanForIntake(ctx, workoutsdb.LatestPlanForIntakeParams{UserID: userID, IntakeID: intakeID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StoredPlan{}, apperr.ErrNotFound
		}
		return StoredPlan{}, apperr.Wrap(err, "latest plan for intake")
	}
	return planFromDB(row)
}

// ListCurrentPlans returns one row per plan — the version being followed —
// where ListPlans returns every version of every plan.
func (r *Repository) ListCurrentPlans(ctx context.Context, userID uuid.UUID, limit int) ([]StoredPlan, error) {
	rows, err := r.q.ListCurrentPlans(ctx, workoutsdb.ListCurrentPlansParams{UserID: userID, Limit: int32(limit)})
	if err != nil {
		return nil, apperr.Wrap(err, "list current plans")
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
		Imported:  row.Imported,
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
		ID:         row.ID,
		UserID:     row.UserID,
		IntakeID:   row.IntakeID,
		Plan:       plan,
		Model:      row.Model,
		Provider:   row.Provider,
		Source:     row.Source,
		EditedFrom: row.EditedFrom,
		CreatedAt:  row.CreatedAt,
	}, nil
}

// ActivePlan is the newest version of the plan someone follows. An account
// with plans but no recorded choice — one that predates the choice, or whose
// chosen plan was deleted — follows its newest plan, which is what "active"
// meant before it was a choice.
func (r *Repository) ActivePlan(ctx context.Context, userID uuid.UUID) (StoredPlan, error) {
	row, err := r.q.GetActivePlan(ctx, userID)
	if err == nil {
		return planFromDB(row)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return StoredPlan{}, apperr.Wrap(err, "active plan")
	}
	return r.LatestPlan(ctx, userID)
}

func (r *Repository) SetActivePlan(ctx context.Context, userID, intakeID uuid.UUID) error {
	if err := r.q.SetActivePlan(ctx, workoutsdb.SetActivePlanParams{UserID: userID, IntakeID: intakeID}); err != nil {
		return apperr.Wrap(err, "set active plan")
	}
	return nil
}

// StoredWeek is a recorded week of training. Start is its Monday.
type StoredWeek struct {
	Start  time.Time
	Slots  []Slot
	Custom bool
}

// GetWeek is the recorded week starting on start; ok is false when none is.
func (r *Repository) GetWeek(ctx context.Context, userID uuid.UUID, start time.Time) (StoredWeek, bool, error) {
	row, err := r.q.GetWeek(ctx, workoutsdb.GetWeekParams{UserID: userID, WeekStart: dateOf(start)})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StoredWeek{}, false, nil
		}
		return StoredWeek{}, false, apperr.Wrap(err, "get week")
	}
	week, err := weekFromDB(row)
	return week, err == nil, err
}

// InsertWeekIfAbsent records a default week unless one is already recorded.
func (r *Repository) InsertWeekIfAbsent(ctx context.Context, userID uuid.UUID, start time.Time, slots []Slot) error {
	raw, err := encodeSlots(slots)
	if err != nil {
		return err
	}
	if err := r.q.InsertWeekIfAbsent(ctx, workoutsdb.InsertWeekIfAbsentParams{UserID: userID, WeekStart: dateOf(start), Slots: raw}); err != nil {
		return apperr.Wrap(err, "insert week")
	}
	return nil
}

func (r *Repository) UpsertWeek(ctx context.Context, userID uuid.UUID, week StoredWeek) error {
	raw, err := encodeSlots(week.Slots)
	if err != nil {
		return err
	}
	if err := r.q.UpsertWeek(ctx, workoutsdb.UpsertWeekParams{
		UserID: userID, WeekStart: dateOf(week.Start), Slots: raw, Custom: week.Custom,
	}); err != nil {
		return apperr.Wrap(err, "save week")
	}
	return nil
}

func (r *Repository) DeleteWeek(ctx context.Context, userID uuid.UUID, start time.Time) error {
	if err := r.q.DeleteWeek(ctx, workoutsdb.DeleteWeekParams{UserID: userID, WeekStart: dateOf(start)}); err != nil {
		return apperr.Wrap(err, "delete week")
	}
	return nil
}

// PreviousWeekWithIntake is the latest recorded week before start that had
// any of intake's sessions; ok is false when none did.
func (r *Repository) PreviousWeekWithIntake(ctx context.Context, userID uuid.UUID, start time.Time, intakeID uuid.UUID) (StoredWeek, bool, error) {
	row, err := r.q.PreviousWeekWithIntake(ctx, workoutsdb.PreviousWeekWithIntakeParams{
		UserID: userID, WeekStart: dateOf(start), IntakeID: intakeID.String(),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return StoredWeek{}, false, nil
		}
		return StoredWeek{}, false, apperr.Wrap(err, "previous week")
	}
	week, err := weekFromDB(row)
	return week, err == nil, err
}

// WeeksBetween are the recorded weeks starting in [since, until).
func (r *Repository) WeeksBetween(ctx context.Context, userID uuid.UUID, since, until time.Time) ([]StoredWeek, error) {
	rows, err := r.q.ListWeeksBetween(ctx, workoutsdb.ListWeeksBetweenParams{
		UserID: userID, WeekStart: dateOf(since), WeekStart_2: dateOf(until),
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list weeks")
	}
	out := make([]StoredWeek, 0, len(rows))
	for _, row := range rows {
		week, err := weekFromDB(row)
		if err != nil {
			return nil, err
		}
		out = append(out, week)
	}
	return out, nil
}

func weekFromDB(row workoutsdb.WorkoutWeek) (StoredWeek, error) {
	var slots []Slot
	if err := json.Unmarshal(row.Slots, &slots); err != nil {
		return StoredWeek{}, apperr.Wrap(err, "decode week %s", row.WeekStart.Time.Format(time.DateOnly))
	}
	return StoredWeek{Start: row.WeekStart.Time, Slots: slots, Custom: row.Custom}, nil
}

func encodeSlots(slots []Slot) ([]byte, error) {
	if slots == nil {
		slots = []Slot{}
	}
	raw, err := json.Marshal(slots)
	if err != nil {
		return nil, apperr.Wrap(err, "encode week")
	}
	return raw, nil
}

// dateOf is t's calendar date as Postgres stores a date: the day t names in
// its own location, with no time zone attached.
func dateOf(t time.Time) pgtype.Date {
	return pgtype.Date{Time: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), Valid: true}
}
