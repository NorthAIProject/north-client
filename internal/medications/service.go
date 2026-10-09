package medications

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/medications/medication"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

type Service struct {
	repo *Repository
	now  func() time.Time
}

func NewService(repo *Repository) *Service { return &Service{repo: repo, now: time.Now} }

// WithClock fixes now so tests can stand at a given minute of a given day.
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// Patch changes some of a medication's fields; nil leaves one as it is.
type Patch struct {
	Name   *string
	Dose   *string
	Times  *[]string
	Days   *[]int
	Remind *bool
	Notes  *string
}

func (s *Service) Add(ctx context.Context, user users.User, in Input) (Medication, error) {
	clean, err := medication.Validate(in)
	if err != nil {
		return Medication{}, err
	}
	return s.repo.Create(ctx, user.ID, clean)
}

// Update applies a patch to an active medication.
func (s *Service) Update(ctx context.Context, user users.User, id uuid.UUID, p Patch) (Medication, error) {
	current, err := s.active(ctx, user, id)
	if err != nil {
		return Medication{}, err
	}
	in := Input{
		Name: current.Name, Dose: current.Dose, Times: current.Times, Days: current.Days,
		Remind: current.Remind, Notes: current.Notes,
	}
	if p.Name != nil {
		in.Name = *p.Name
	}
	if p.Dose != nil {
		in.Dose = *p.Dose
	}
	if p.Times != nil {
		in.Times = *p.Times
	}
	if p.Days != nil {
		in.Days = *p.Days
	}
	if p.Remind != nil {
		in.Remind = *p.Remind
	}
	if p.Notes != nil {
		in.Notes = *p.Notes
	}
	clean, err := medication.Validate(in)
	if err != nil {
		return Medication{}, err
	}
	return s.repo.Update(ctx, id, user.ID, clean)
}

// Stop ends a medication. Its doses stay, as history.
func (s *Service) Stop(ctx context.Context, user users.User, id uuid.UUID) (Medication, error) {
	return s.repo.Stop(ctx, id, user.ID, s.now())
}

func (s *Service) List(ctx context.Context, user users.User, activeOnly bool) ([]Medication, error) {
	return s.repo.List(ctx, user.ID, activeOnly)
}

// FindByName resolves a name as someone says it among the active
// medications. Ambiguity is a validation error naming the candidates rather
// than a guess, because logging against the wrong medication is silent.
func (s *Service) FindByName(ctx context.Context, user users.User, name string) (Medication, error) {
	meds, err := s.repo.List(ctx, user.ID, true)
	if err != nil {
		return Medication{}, err
	}
	hits := medication.Matching(meds, name)
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		if len(meds) == 0 {
			return Medication{}, apperr.Wrap(apperr.ErrNotFound, "no medications are tracked yet")
		}
		return Medication{}, apperr.Wrap(apperr.ErrNotFound, "no active medication matches %q; tracked: %s",
			name, strings.Join(medication.Names(meds), ", "))
	default:
		return Medication{}, apperr.FieldErrors{}.Add("name",
			"\""+name+"\" matches several medications ("+strings.Join(medication.Names(hits), ", ")+"); be more specific")
	}
}

// LogDose records a dose of an active medication as taken or skipped. With no
// slot, the nearest unanswered time today is used; an as-needed medication's
// dose never has one.
func (s *Service) LogDose(ctx context.Context, user users.User, medID uuid.UUID, status string, slot *string) (Dose, error) {
	if status == "" {
		status = medication.StatusTaken
	}
	if !medication.ValidStatus(status) {
		return Dose{}, apperr.FieldErrors{}.Add("status", "A dose is taken or skipped.")
	}
	med, err := s.active(ctx, user, medID)
	if err != nil {
		return Dose{}, err
	}

	now := s.now()
	local := now.In(user.Location())
	date := timerange.StartOfDay(local)

	switch {
	case med.AsNeeded():
		slot = nil
	case slot != nil:
		t, ok := medication.NormalizeTime(*slot)
		if !ok {
			return Dose{}, apperr.FieldErrors{}.Add("slot", "The time is HH:MM, like 08:00.")
		}
		today := medication.SlotsFor(med, local.Weekday())
		if !slices.Contains(today, t) {
			if len(today) == 0 {
				return Dose{}, apperr.FieldErrors{}.Add("slot", med.Name+" is not scheduled today; log it without a time.")
			}
			return Dose{}, apperr.FieldErrors{}.Add("slot", med.Name+" is scheduled at "+strings.Join(today, ", ")+" today.")
		}
		slot = &t
	default:
		logs, err := s.repo.DosesOn(ctx, user.ID, date)
		if err != nil {
			return Dose{}, err
		}
		picked, ok := medication.PickSlot(med, logs, local)
		if !ok {
			return Dose{}, apperr.FieldErrors{}.Add("slot", "Every dose of "+med.Name+" today is already logged ("+
				strings.Join(med.Times, ", ")+"); name the time to change one.")
		}
		slot = picked
	}

	dose, err := s.repo.LogDose(ctx, user.ID, med.ID, date, slot, status, now)
	if err != nil {
		return Dose{}, err
	}
	dose.Name, dose.DoseText = med.Name, med.Dose
	return dose, nil
}

// UndoDose removes a logged dose, which puts its slot back to unanswered.
func (s *Service) UndoDose(ctx context.Context, user users.User, logID uuid.UUID) error {
	return s.repo.DeleteDose(ctx, logID, user.ID)
}

// Today is every active medication against what was logged today.
func (s *Service) Today(ctx context.Context, user users.User) (Day, error) {
	return s.dayAt(ctx, user, s.now().In(user.Location()))
}

// TodayDoses is every dose logged today, newest first, for undoing one.
func (s *Service) TodayDoses(ctx context.Context, user users.User) ([]Dose, error) {
	return s.repo.DosesOn(ctx, user.ID, timerange.StartOfDay(s.now().In(user.Location())))
}

// DueReminders are the scheduled doses whose time has come within the last
// two hours, that nobody answered, of medications set to remind. The nudge
// sweep raises one for each.
func (s *Service) DueReminders(ctx context.Context, user users.User, now time.Time) ([]Reminder, error) {
	day, err := s.dayAt(ctx, user, now.In(user.Location()))
	if err != nil {
		return nil, err
	}
	return medication.DueReminders(day, medication.ReminderWindow), nil
}

// History is every medication, stopped ones included, and every dose ever
// logged: what an export needs.
func (s *Service) History(ctx context.Context, user users.User) ([]Medication, []Dose, error) {
	meds, err := s.repo.List(ctx, user.ID, false)
	if err != nil {
		return nil, nil, err
	}
	doses, err := s.repo.AllDoses(ctx, user.ID)
	if err != nil {
		return nil, nil, err
	}
	return meds, doses, nil
}

func (s *Service) dayAt(ctx context.Context, user users.User, local time.Time) (Day, error) {
	meds, err := s.repo.List(ctx, user.ID, true)
	if err != nil {
		return Day{}, err
	}
	logs, err := s.repo.DosesOn(ctx, user.ID, timerange.StartOfDay(local))
	if err != nil {
		return Day{}, err
	}
	return medication.BuildDay(meds, logs, local), nil
}

func (s *Service) active(ctx context.Context, user users.User, id uuid.UUID) (Medication, error) {
	med, err := s.repo.Get(ctx, id, user.ID)
	if err != nil {
		return Medication{}, err
	}
	if !med.Active() {
		return Medication{}, apperr.Wrap(apperr.ErrNotFound, "%s was stopped", med.Name)
	}
	return med, nil
}
