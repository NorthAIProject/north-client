package medications

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	medicationsdb "github.com/NorthAIProject/north-client/internal/medications/db"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
)

type Repository struct {
	q *medicationsdb.Queries
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{q: medicationsdb.New(pool)}
}

func (r *Repository) Create(ctx context.Context, userID uuid.UUID, in Input) (Medication, error) {
	row, err := r.q.CreateMedication(ctx, medicationsdb.CreateMedicationParams{
		UserID: userID, Name: in.Name, Dose: in.Dose, TimesOfDay: nonNil(in.Times),
		DaysOfWeek: toInt16(in.Days), Remind: in.Remind, Notes: in.Notes,
	})
	if err != nil {
		return Medication{}, nameTaken(err, in.Name, "create medication")
	}
	return fromDB(row), nil
}

func (r *Repository) Update(ctx context.Context, id, userID uuid.UUID, in Input) (Medication, error) {
	row, err := r.q.UpdateMedication(ctx, medicationsdb.UpdateMedicationParams{
		ID: id, UserID: userID, Name: in.Name, Dose: in.Dose, TimesOfDay: nonNil(in.Times),
		DaysOfWeek: toInt16(in.Days), Remind: in.Remind, Notes: in.Notes,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Medication{}, apperr.ErrNotFound
	}
	if err != nil {
		return Medication{}, nameTaken(err, in.Name, "update medication")
	}
	return fromDB(row), nil
}

func (r *Repository) Stop(ctx context.Context, id, userID uuid.UUID, at time.Time) (Medication, error) {
	row, err := r.q.StopMedication(ctx, medicationsdb.StopMedicationParams{ID: id, UserID: userID, StoppedAt: &at})
	if errors.Is(err, pgx.ErrNoRows) {
		return Medication{}, apperr.ErrNotFound
	}
	if err != nil {
		return Medication{}, apperr.Wrap(err, "stop medication")
	}
	return fromDB(row), nil
}

func (r *Repository) Get(ctx context.Context, id, userID uuid.UUID) (Medication, error) {
	row, err := r.q.GetMedication(ctx, medicationsdb.GetMedicationParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Medication{}, apperr.ErrNotFound
	}
	if err != nil {
		return Medication{}, apperr.Wrap(err, "get medication")
	}
	return fromDB(row), nil
}

func (r *Repository) List(ctx context.Context, userID uuid.UUID, activeOnly bool) ([]Medication, error) {
	var (
		rows []medicationsdb.Medication
		err  error
	)
	if activeOnly {
		rows, err = r.q.ListActiveMedications(ctx, userID)
	} else {
		rows, err = r.q.ListAllMedications(ctx, userID)
	}
	if err != nil {
		return nil, apperr.Wrap(err, "list medications")
	}
	out := make([]Medication, len(rows))
	for i, row := range rows {
		out[i] = fromDB(row)
	}
	return out, nil
}

// LogDose records a dose. A scheduled slot logged again keeps the newer
// status; an unscheduled dose (nil slot) is always a new row.
func (r *Repository) LogDose(ctx context.Context, userID, medID uuid.UUID, date time.Time, slot *string, status string, at time.Time) (Dose, error) {
	row, err := r.q.UpsertMedicationLog(ctx, medicationsdb.UpsertMedicationLogParams{
		UserID: userID, MedicationID: medID, LogDate: pgtype.Date{Time: date, Valid: true},
		Slot: slot, Status: status, LoggedAt: at,
	})
	if err != nil {
		return Dose{}, apperr.Wrap(err, "log medication dose")
	}
	return Dose{
		ID: row.ID, MedicationID: row.MedicationID, LogDate: row.LogDate.Time,
		Slot: row.Slot, Status: row.Status, LoggedAt: row.LoggedAt,
	}, nil
}

func (r *Repository) DeleteDose(ctx context.Context, id, userID uuid.UUID) error {
	n, err := r.q.DeleteMedicationLog(ctx, medicationsdb.DeleteMedicationLogParams{ID: id, UserID: userID})
	if err != nil {
		return apperr.Wrap(err, "delete medication dose")
	}
	if n == 0 {
		return apperr.ErrNotFound
	}
	return nil
}

// DosesOn is every dose logged on a local date, newest first.
func (r *Repository) DosesOn(ctx context.Context, userID uuid.UUID, date time.Time) ([]Dose, error) {
	rows, err := r.q.ListMedicationLogsOn(ctx, medicationsdb.ListMedicationLogsOnParams{
		UserID: userID, LogDate: pgtype.Date{Time: date, Valid: true},
	})
	if err != nil {
		return nil, apperr.Wrap(err, "list medication doses")
	}
	out := make([]Dose, len(rows))
	for i, row := range rows {
		out[i] = Dose{
			ID: row.ID, MedicationID: row.MedicationID, Name: row.MedicationName, DoseText: row.MedicationDose,
			LogDate: row.LogDate.Time, Slot: row.Slot, Status: row.Status, LoggedAt: row.LoggedAt,
		}
	}
	return out, nil
}

// AllDoses is every dose ever logged, newest day first.
func (r *Repository) AllDoses(ctx context.Context, userID uuid.UUID) ([]Dose, error) {
	rows, err := r.q.ListAllMedicationLogs(ctx, userID)
	if err != nil {
		return nil, apperr.Wrap(err, "list medication history")
	}
	out := make([]Dose, len(rows))
	for i, row := range rows {
		out[i] = Dose{
			ID: row.ID, MedicationID: row.MedicationID, Name: row.MedicationName, DoseText: row.MedicationDose,
			LogDate: row.LogDate.Time, Slot: row.Slot, Status: row.Status, LoggedAt: row.LoggedAt,
		}
	}
	return out, nil
}

// nameTaken reports the active-name unique index as the form error it is.
func nameTaken(err error, name, op string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return apperr.FieldErrors{}.Add("name", "You already track "+name+".")
	}
	return apperr.Wrap(err, "%s", op)
}

func fromDB(row medicationsdb.Medication) Medication {
	days := make([]int, len(row.DaysOfWeek))
	for i, d := range row.DaysOfWeek {
		days[i] = int(d)
	}
	return Medication{
		ID: row.ID, Name: row.Name, Dose: row.Dose, Times: row.TimesOfDay, Days: days,
		Remind: row.Remind, Notes: row.Notes, StoppedAt: row.StoppedAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func toInt16(days []int) []int16 {
	out := make([]int16, len(days))
	for i, d := range days {
		out[i] = int16(d)
	}
	return out
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
