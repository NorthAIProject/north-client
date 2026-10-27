package checkins

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/awards/award"
	"github.com/NorthAIProject/north-client/internal/checkins/checkin"
	"github.com/NorthAIProject/north-client/internal/goals"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/users"
)

const (
	contextDays = 14
	// contextLimit is a safety bound on the query, not a second window: it must
	// stay >= contextDays, or a user who checks in daily silently loses the
	// older days the coach has been told it can see.
	contextLimit = contextDays
	listDefault  = 30
	streakLook   = 400
)

// GoalLookup verifies optional goal links belong to the user.
type GoalLookup interface {
	Get(ctx context.Context, id, userID uuid.UUID) (goals.Goal, error)
}

// SyncHook is notified when check-ins are saved to synchronize state across clients.
// Awards checks a person's badges after a write. awards.Service satisfies it.
type Awards interface {
	Check(ctx context.Context, userID uuid.UUID, at time.Time, areas ...award.Area)
}

type SyncHook interface {
	OnCheckInSaved(ctx context.Context, userID uuid.UUID) error
}

type Service struct {
	repo         *Repository
	goals        GoalLookup
	sync         SyncHook
	achievements Achievements
	awards       Awards
}

// Achievements records check-in streaks worth showing friends.
// achievements.Service satisfies it; it ignores lengths that are not marks.
type Achievements interface {
	StreakReached(ctx context.Context, userID uuid.UUID, days int, localDate string, at time.Time)
}

func (s *Service) WithAchievements(a Achievements) *Service {
	s.achievements = a
	return s
}

func (s *Service) WithAwards(a Awards) *Service {
	s.awards = a
	return s
}

func NewService(repo *Repository, goals GoalLookup) *Service {
	return &Service{repo: repo, goals: goals}
}

func (s *Service) WithSync(hook SyncHook) *Service {
	s.sync = hook
	return s
}

// Input is a check-in as submitted.
type Input struct {
	Mood, Energy  int
	Wins          string
	Challenges    string
	Notes         string
	RelatedGoalID *uuid.UUID

	// Stress and SleepQuality are optional 1–5 scales; nil means not given.
	Stress       *int
	SleepQuality *int
	Tags         []string

	// Source is the surface writing. It is stored only when this write
	// creates the day's check-in; edits keep the original. Blank is unknown.
	Source checkin.Source
}

// Validate checks a check-in before it is stored.
func Validate(in Input) (Input, error) {
	var errs apperr.FieldErrors

	if in.Mood < 1 || in.Mood > 5 {
		errs = errs.Add("mood", "Pick a mood from 1 to 5.")
	}
	if in.Energy < 1 || in.Energy > 5 {
		errs = errs.Add("energy", "Pick an energy level from 1 to 5.")
	}
	if in.Stress != nil && (*in.Stress < 1 || *in.Stress > 5) {
		errs = errs.Add("stress", "Pick a stress level from 1 to 5.")
	}
	if in.SleepQuality != nil && (*in.SleepQuality < 1 || *in.SleepQuality > 5) {
		errs = errs.Add("sleep_quality", "Pick a sleep quality from 1 to 5.")
	}

	in.Wins = strings.TrimSpace(in.Wins)
	if len(in.Wins) > 500 {
		errs = errs.Add("wins", "Keep wins under 500 characters.")
	}
	in.Challenges = strings.TrimSpace(in.Challenges)
	if len(in.Challenges) > 500 {
		errs = errs.Add("challenges", "Keep challenges under 500 characters.")
	}
	in.Notes = strings.TrimSpace(in.Notes)
	if len(in.Notes) > 1000 {
		errs = errs.Add("notes", "Keep notes under 1000 characters.")
	}

	in.Tags = checkin.NormalizeTags(in.Tags)
	if len(in.Tags) > checkin.MaxTags {
		errs = errs.Add("tags", fmt.Sprintf("Use at most %d tags.", checkin.MaxTags))
	}
	for _, tag := range in.Tags {
		if utf8.RuneCountInString(tag) > checkin.MaxTagLength {
			errs = errs.Add("tags", fmt.Sprintf("Keep each tag to %d characters.", checkin.MaxTagLength))
			break
		}
	}

	if in.Source == "" {
		in.Source = checkin.SourceUnknown
	}

	return in, errs.OrNil()
}

// LocalDate is the calendar day for this user at the given instant.
func LocalDate(user users.User, at time.Time) time.Time {
	loc := user.Location()
	t := at.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

// Saved is a check-in just stored, with what the save means for the run.
type Saved struct {
	CheckIn
	// Created is true when this save made today's check-in, false when it
	// replaced one. Only a creating save earns a moment.
	Created bool
	Run     checkin.Run
}

// UpsertToday creates or replaces today's check-in in the user's timezone.
// Every content field is written as given: this is the full form (web or app),
// where a field left blank is a deliberate clear. A writer that knows only some
// fields uses MergeToday instead. The source is kept from the first save.
func (s *Service) UpsertToday(ctx context.Context, user users.User, in Input) (Saved, error) {
	clean, err := Validate(in)
	if err != nil {
		return Saved{}, err
	}
	if err = s.checkGoal(ctx, user.ID, clean.RelatedGoalID); err != nil {
		return Saved{}, err
	}
	return s.saveToday(ctx, user, clean)
}

// MergeToday records a partial check-in: whatever the writer gave replaces
// today's value, and everything it left blank keeps what is already there.
//
// The coach, MCP and capture write through here. They know mood and energy and
// perhaps a note; replacing the whole row with that erased the wins and
// challenges written on the form that morning.
func (s *Service) MergeToday(ctx context.Context, user users.User, in Input) (CheckIn, error) {
	// Only a goal this writer named is checked. One already on today's entry
	// was valid when it was linked, and a goal completed since must not make
	// the coach's check-in fail.
	if err := s.checkGoal(ctx, user.ID, in.RelatedGoalID); err != nil {
		return CheckIn{}, err
	}

	existing, err := s.repo.GetByDate(ctx, user.ID, LocalDate(user, time.Now()))
	switch {
	case err == nil:
		in = mergeInto(existing, in)
	case !apperr.Is(err, apperr.ErrNotFound):
		return CheckIn{}, err
	}

	clean, err := Validate(in)
	if err != nil {
		return CheckIn{}, err
	}
	saved, err := s.saveToday(ctx, user, clean)
	return saved.CheckIn, err
}

// mergeInto lays a partial input over an existing check-in. Mood and energy
// always come from the writer; every optional field does only when it was
// given. Source is the writer's, though the store keeps the original.
func mergeInto(base CheckIn, patch Input) Input {
	out := Input{
		Mood:          patch.Mood,
		Energy:        patch.Energy,
		Source:        patch.Source,
		Wins:          base.Wins,
		Challenges:    base.Challenges,
		Notes:         base.Notes,
		RelatedGoalID: base.RelatedGoalID,
		Stress:        base.Stress,
		SleepQuality:  base.SleepQuality,
		Tags:          base.Tags,
	}
	if strings.TrimSpace(patch.Wins) != "" {
		out.Wins = patch.Wins
	}
	if strings.TrimSpace(patch.Challenges) != "" {
		out.Challenges = patch.Challenges
	}
	if strings.TrimSpace(patch.Notes) != "" {
		out.Notes = patch.Notes
	}
	if patch.RelatedGoalID != nil {
		out.RelatedGoalID = patch.RelatedGoalID
	}
	if patch.Stress != nil {
		out.Stress = patch.Stress
	}
	if patch.SleepQuality != nil {
		out.SleepQuality = patch.SleepQuality
	}
	if len(checkin.NormalizeTags(patch.Tags)) > 0 {
		out.Tags = patch.Tags
	}
	return out
}

// saveToday stores an already validated check-in for today and tells the
// hooks about it.
func (s *Service) saveToday(ctx context.Context, user users.User, clean Input) (Saved, error) {
	now := time.Now()
	checkIn, err := s.repo.Upsert(ctx, user.ID, writeFrom(LocalDate(user, now), clean))
	if err != nil {
		return Saved{}, err
	}
	// Both timestamps default to the insert's now(); a replacing save moves
	// updated_at on, so equal means this save created the row.
	saved := Saved{CheckIn: checkIn, Created: checkIn.CreatedAt.Equal(checkIn.UpdatedAt)}
	if s.sync != nil {
		_ = s.sync.OnCheckInSaved(ctx, user.ID)
	}
	if s.awards != nil {
		s.awards.Check(ctx, user.ID, now, award.Consistency)
	}
	// The save already happened: a run that cannot be read leaves the
	// response without its streak rather than failing it.
	if run, runErr := s.RunAt(ctx, user, now); runErr == nil {
		saved.Run = run
		if s.achievements != nil {
			s.achievements.StreakReached(ctx, user.ID, run.Current, LocalDate(user, now).Format("2006-01-02"), now)
		}
	}
	return saved, nil
}

func writeFrom(localDate time.Time, clean Input) Write {
	return Write{
		LocalDate:     localDate,
		Mood:          clean.Mood,
		Energy:        clean.Energy,
		Wins:          clean.Wins,
		Challenges:    clean.Challenges,
		Notes:         clean.Notes,
		RelatedGoalID: clean.RelatedGoalID,
		Stress:        clean.Stress,
		SleepQuality:  clean.SleepQuality,
		Tags:          clean.Tags,
		Source:        clean.Source,
	}
}

// Version fingerprints this person's check-ins. It changes on every save,
// edit and delete, so a display holding the old value knows it is stale.
func (s *Service) Version(ctx context.Context, userID uuid.UUID) (string, error) {
	return s.repo.Version(ctx, userID)
}

func (s *Service) Get(ctx context.Context, id, userID uuid.UUID) (CheckIn, error) {
	return s.repo.Get(ctx, id, userID)
}

func (s *Service) Today(ctx context.Context, user users.User) (CheckIn, error) {
	c, err := s.repo.GetByDate(ctx, user.ID, LocalDate(user, time.Now()))
	if err != nil {
		return CheckIn{}, err
	}
	// The goal's title, as List fills it, so today's check-in reads the same.
	return s.withGoalTitles(ctx, user.ID, []CheckIn{c})[0], nil
}

// ListBetween returns every check-in inside a window, newest first. Unlike
// List it takes no limit: the window bounds the result.
func (s *Service) ListBetween(ctx context.Context, userID uuid.UUID, rg timerange.Range) ([]CheckIn, error) {
	list, err := s.repo.ListBetween(ctx, userID, rg.Since, rg.Until)
	if err != nil {
		return nil, err
	}
	return s.withGoalTitles(ctx, userID, list), nil
}

func (s *Service) List(ctx context.Context, userID uuid.UUID, limit int) ([]CheckIn, error) {
	if limit <= 0 || limit > 100 {
		limit = listDefault
	}
	list, err := s.repo.List(ctx, userID, limit)
	if err != nil {
		return nil, err
	}
	return s.withGoalTitles(ctx, userID, list), nil
}

func (s *Service) Update(ctx context.Context, id, userID uuid.UUID, in Input) (CheckIn, error) {
	clean, err := Validate(in)
	if err != nil {
		return CheckIn{}, err
	}
	if err = s.checkGoal(ctx, userID, clean.RelatedGoalID); err != nil {
		return CheckIn{}, err
	}
	checkIn, err := s.repo.Update(ctx, id, userID, writeFrom(time.Time{}, clean))
	if err != nil {
		return CheckIn{}, err
	}
	if s.sync != nil {
		_ = s.sync.OnCheckInSaved(ctx, userID)
	}
	return checkIn, nil
}

// Delete removes a check-in. It only affects rows the caller owns.
func (s *Service) Delete(ctx context.Context, id, userID uuid.UUID) error {
	return s.repo.Delete(ctx, id, userID)
}

// LatestLocalDate is the most recent check-in calendar day, if any.
func (s *Service) LatestLocalDate(ctx context.Context, userID uuid.UUID) (time.Time, bool, error) {
	dates, err := s.repo.Dates(ctx, userID, 1)
	if err != nil {
		return time.Time{}, false, err
	}
	if len(dates) == 0 {
		return time.Time{}, false, nil
	}
	return dates[0], true, nil
}

// RecentForContext returns the last two weeks of check-ins for the coach.
func (s *Service) RecentForContext(ctx context.Context, user users.User) ([]CheckIn, error) {
	since := LocalDate(user, time.Now()).AddDate(0, 0, -(contextDays - 1))
	list, err := s.repo.ListSince(ctx, user.ID, since, contextLimit)
	if err != nil {
		return nil, err
	}
	return s.withGoalTitles(ctx, user.ID, list), nil
}

// withGoalTitles fills RelatedGoalTitle on each check-in that links to a
// goal. Lookups are best-effort: a goal that fails to resolve (deleted,
// lookup unavailable) leaves the title blank rather than failing the list.
func (s *Service) withGoalTitles(ctx context.Context, userID uuid.UUID, list []CheckIn) []CheckIn {
	if s.goals == nil {
		return list
	}
	titles := make(map[uuid.UUID]string)
	for i, c := range list {
		if c.RelatedGoalID == nil {
			continue
		}
		title, ok := titles[*c.RelatedGoalID]
		if !ok {
			g, err := s.goals.Get(ctx, *c.RelatedGoalID, userID)
			if err != nil {
				continue
			}
			title = g.Title
			titles[*c.RelatedGoalID] = title
		}
		list[i].RelatedGoalTitle = title
	}
	return list
}

// Streak is consecutive local days with a check-in, ending today or yesterday.
// Zero is not failure — it is "not started", and the UI should stay quiet.
func (s *Service) Streak(ctx context.Context, user users.User) (int, error) {
	return s.StreakAt(ctx, user, time.Now())
}

// StreakAt is Streak as of a given instant. The nudge sweep passes its own
// clock so "is the streak about to break tonight" can be tested without
// waiting for tonight. It is the run's current count, rest days included, so
// every caller agrees with what the check-ins page shows.
func (s *Service) StreakAt(ctx context.Context, user users.User, now time.Time) (int, error) {
	run, err := s.RunAt(ctx, user, now)
	return run.Current, err
}

// Run is the person's check-in run with its rest days, as of now.
func (s *Service) Run(ctx context.Context, user users.User) (checkin.Run, error) {
	return s.RunAt(ctx, user, time.Now())
}

// RunAt is Run as of a given instant. See checkin.ComputeRun for the rules.
func (s *Service) RunAt(ctx context.Context, user users.User, now time.Time) (checkin.Run, error) {
	dates, err := s.repo.Dates(ctx, user.ID, streakLook)
	if err != nil {
		return checkin.Run{}, err
	}
	return checkin.ComputeRun(dates, LocalDate(user, now), checkin.RestsFrom), nil
}

func (s *Service) checkGoal(ctx context.Context, userID uuid.UUID, goalID *uuid.UUID) error {
	if goalID == nil || s.goals == nil {
		return nil
	}
	g, err := s.goals.Get(ctx, *goalID, userID)
	if err != nil {
		if apperr.Is(err, apperr.ErrNotFound) {
			return apperr.FieldErrors{{Field: "related_goal_id", Message: "That goal is not available."}}
		}
		return err
	}
	if !g.IsActive() {
		return apperr.FieldErrors{{Field: "related_goal_id", Message: "That goal is not available."}}
	}
	return nil
}

// Total is how many days this person has ever checked in. My Day turns it into
// a level; unlike the streak, a missed day never takes it away.
func (s *Service) Total(ctx context.Context, userID uuid.UUID) (int, error) {
	return s.repo.Count(ctx, userID)
}
