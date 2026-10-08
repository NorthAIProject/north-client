package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/FACorreiaa/go-utils/pkg/util"
	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/ai"
	"github.com/NorthAIProject/north-client/internal/caffeine"
	caffeinepkg "github.com/NorthAIProject/north-client/internal/caffeine/caffeine"
	"github.com/NorthAIProject/north-client/internal/fasting"
	"github.com/NorthAIProject/north-client/internal/health"
	"github.com/NorthAIProject/north-client/internal/lifts"
	"github.com/NorthAIProject/north-client/internal/lifts/lift"
	"github.com/NorthAIProject/north-client/internal/screentime"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/shared/timerange"
	"github.com/NorthAIProject/north-client/internal/soreness"
	"github.com/NorthAIProject/north-client/internal/soreness/sore"
	"github.com/NorthAIProject/north-client/internal/supplements"
	"github.com/NorthAIProject/north-client/internal/supplements/supplement"
	"github.com/NorthAIProject/north-client/internal/users"
)

// My Day's trackers, filled in by conversation: "had a double espresso",
// "took my vitamin D", "started fasting at eight", "did 100 kg for 5 on
// squat". Each goes through the slice that owns it, so the chat, Telegram and
// the pages all see the same entry. None are ReadOnly, so each shows an
// approval card first, except get_lift_stats and get_training_context.

func presetKeys[T any](presets []T, key func(T) string) string {
	keys := make([]string, len(presets))
	for i, p := range presets {
		keys[i] = key(p)
	}
	return strings.Join(keys, ", ")
}

func logCaffeine(svc *caffeine.Service, userSvc *users.Service) Capability {
	type args struct {
		Preset string `json:"preset"`
		MG     int    `json:"mg"`
		Label  string `json:"label"`
	}
	return Capability{
		Tool: ai.Tool{
			Name: "log_caffeine",
			Description: "Record a drink with caffeine. Use a preset (" +
				presetKeys(caffeinepkg.Presets(), func(p caffeinepkg.Preset) string { return p.Key }) +
				") or give mg and a label for anything else; a preset with mg overrides the dose.",
			Parameters: ai.Object("the drink", map[string]*ai.Schema{
				"preset": ai.String("a preset key; optional"),
				"mg":     ai.Integer("caffeine in milligrams; optional with a preset"),
				"label":  ai.String("what it was, when not a preset"),
			}),
		},
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}
			user, err := userSvc.ByID(ctx, userID)
			if err != nil {
				return "", err
			}
			entry, err := svc.Log(ctx, user, caffeine.LogInput{Preset: in.Preset, MG: in.MG, Label: in.Label})
			if err != nil {
				return "", err
			}
			today, err := svc.Today(ctx, user)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Logged %s (%d mg). %s", caffeinepkg.DisplayName(entry.Label), entry.MG,
				caffeinepkg.Summary(today, time.Now())), nil
		},
	}
}

func logSupplement(svc *supplements.Service, userSvc *users.Service) Capability {
	type args struct {
		Preset string `json:"preset"`
		Name   string `json:"name"`
		Count  int    `json:"count"`
	}
	return Capability{
		Tool: ai.Tool{
			Name: "log_supplement",
			Description: "Record a supplement taken. Use a preset (" +
				presetKeys(supplement.Presets(), func(p supplement.Preset) string { return p.Key }) +
				") so it counts toward their nutrients, or a name for anything else.",
			Parameters: ai.Object("the supplement", map[string]*ai.Schema{
				"preset": ai.String("a preset key; optional"),
				"name":   ai.String("its name, when not a preset"),
				"count":  ai.Integer("how many doses; defaults to 1"),
			}),
		},
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}
			user, err := userSvc.ByID(ctx, userID)
			if err != nil {
				return "", err
			}
			entry, err := svc.Log(ctx, user, supplements.LogInput{Preset: in.Preset, Name: in.Name, Count: in.Count})
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Logged %s ×%d.", entry.Name, entry.Count), nil
		},
	}
}

func startFast(svc *fasting.Service, userSvc *users.Service) Capability {
	type args struct {
		TargetHours int    `json:"target_hours"`
		StartedAt   string `json:"started_at"`
	}
	return Capability{
		Tool: ai.Tool{
			Name:        "start_fast",
			Description: "Start a fast. Only one can be open; stop_fast ends it. started_at is for a fast that began earlier, as HH:MM today in their time.",
			Parameters: ai.Object("the fast", map[string]*ai.Schema{
				"target_hours": ai.Integer("how long they mean to fast; defaults to 16"),
				"started_at":   ai.String("HH:MM today when it began; optional, defaults to now"),
			}),
		},
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}
			user, err := userSvc.ByID(ctx, userID)
			if err != nil {
				return "", err
			}
			at, err := clockToday(in.StartedAt, user.Location(), time.Now())
			if err != nil {
				return "", err
			}
			session, err := svc.Start(ctx, user, in.TargetHours, at)
			if err != nil {
				return "", err
			}
			return session.Summary(time.Now()), nil
		},
	}
}

func stopFast(svc *fasting.Service, userSvc *users.Service) Capability {
	return Capability{
		Tool: ai.Tool{
			Name:        "stop_fast",
			Description: "End the fast that is running now.",
			Parameters:  ai.Object("nothing to fill in", map[string]*ai.Schema{}),
		},
		Invoke: func(ctx context.Context, userID uuid.UUID, _ json.RawMessage) (string, error) {
			user, err := userSvc.ByID(ctx, userID)
			if err != nil {
				return "", err
			}
			session, err := svc.Stop(ctx, user)
			if err != nil {
				return "", err
			}
			return "Fast ended. " + session.Summary(time.Now()), nil
		},
	}
}

func logScreenTime(svc *screentime.Service, userSvc *users.Service) Capability {
	type args struct {
		Minutes int `json:"minutes"`
	}
	return Capability{
		Tool: ai.Tool{
			Name:        "log_screen_time",
			Description: "Set today's phone screen time, as read from the Screen Time settings. It replaces any figure already there for today.",
			Parameters: ai.Object("the screen time", map[string]*ai.Schema{
				"minutes": ai.Integer("total minutes today"),
			}, "minutes"),
		},
		Idempotent: true,
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}
			user, err := userSvc.ByID(ctx, userID)
			if err != nil {
				return "", err
			}
			if _, err := svc.Set(ctx, user, nil, in.Minutes, "manual"); err != nil {
				return "", err
			}
			return fmt.Sprintf("Screen time today set to %dh %dm.", in.Minutes/60, in.Minutes%60), nil
		},
	}
}

func recordSoreness(svc *soreness.Service, userSvc *users.Service) Capability {
	type args struct {
		Region   string `json:"region"`
		Severity int    `json:"severity"`
		Note     string `json:"note"`
	}
	return Capability{
		Tool: ai.Tool{
			Name:        "record_soreness",
			Description: "Record a sore body region for today, which lights it on the body in My Day and informs training advice.",
			Parameters: ai.Object("where and how sore", map[string]*ai.Schema{
				"region":   ai.Enum("the body region", sore.Regions()...),
				"severity": ai.Integer("1 stiff, 2 sore, 3 painful"),
				"note":     ai.String("anything they said about it; optional"),
			}, "region", "severity"),
		},
		Idempotent: true,
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}
			user, err := userSvc.ByID(ctx, userID)
			if err != nil {
				return "", err
			}
			if _, err := svc.Set(ctx, user, in.Region, in.Severity, in.Note); err != nil {
				return "", err
			}
			return fmt.Sprintf("Recorded %s as sore (%d of 3) for today.", strings.ReplaceAll(in.Region, "_", " "), in.Severity), nil
		},
	}
}

func recordBloodPressure(svc *health.Service) Capability {
	type args struct {
		Systolic  int `json:"systolic"`
		Diastolic int `json:"diastolic"`
	}
	return Capability{
		Tool: ai.Tool{
			Name:        "record_blood_pressure",
			Description: "Record a blood pressure reading taken now, such as 120/80.",
			Parameters: ai.Object("the reading", map[string]*ai.Schema{
				"systolic":  ai.Integer("the top number, mmHg"),
				"diastolic": ai.Integer("the bottom number, mmHg"),
			}, "systolic", "diastolic"),
		},
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}
			if _, err := svc.RecordBloodPressure(ctx, userID, in.Systolic, in.Diastolic, nil); err != nil {
				return "", err
			}
			return fmt.Sprintf("Recorded blood pressure %d/%d.", in.Systolic, in.Diastolic), nil
		},
	}
}

func logLiftSet(svc *lifts.Service, userSvc *users.Service) Capability {
	type args struct {
		Exercise  string  `json:"exercise"`
		Slug      string  `json:"slug"`
		WeightKg  float64 `json:"weight_kg"`
		Reps      int     `json:"reps"`
		Sets      int     `json:"sets"`
		SetNumber int     `json:"set_number"`
	}
	return Capability{
		Tool: ai.Tool{
			Name: "log_lift_set",
			Description: "Record strength sets done today: the exercise, the weight in kilograms (convert pounds; 0 for bodyweight) and reps. " +
				"sets repeats the same weight and reps, for '3 sets of 5 at 100'. Numbering continues from today's sets of that exercise.",
			Parameters: ai.Object("what was lifted", map[string]*ai.Schema{
				"exercise":   ai.String("the exercise's name"),
				"slug":       ai.String("its catalog slug from search_exercises, when known; optional"),
				"weight_kg":  ai.Number("weight in kilograms"),
				"reps":       ai.Integer("reps in each set"),
				"sets":       ai.Integer("how many sets at this weight and reps; defaults to 1"),
				"set_number": ai.Integer("the number of the first set, when they said; optional"),
			}, "exercise", "weight_kg", "reps"),
		},
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}
			user, err := userSvc.ByID(ctx, userID)
			if err != nil {
				return "", err
			}
			count := max(1, min(in.Sets, 10))
			number := in.SetNumber
			if number < 1 {
				if number, err = svc.NextSetNumber(ctx, user, in.Slug, in.Exercise); err != nil {
					return "", err
				}
			}
			var last lifts.Set
			for i := range count {
				if last, err = svc.Log(ctx, user, lifts.LogInput{
					ExerciseName: in.Exercise, ExerciseSlug: in.Slug, SetNumber: number + i, WeightKg: in.WeightKg, Reps: in.Reps,
				}); err != nil {
					return "", err
				}
			}
			return fmt.Sprintf("Logged %d set(s) of %s: %.1f kg × %d (est. 1RM %.1f kg).",
				count, last.ExerciseName, last.WeightKg, last.Reps, util.RoundHalfUpToScale(last.E1RM(), 1)), nil
		},
	}
}

func getLiftStats(svc *lifts.Service, userSvc *users.Service) Capability {
	type args struct {
		Range string `json:"range"`
	}
	return Capability{
		Tool: ai.Tool{
			Name:        "get_lift_stats",
			Description: "Read their lifting over a window: volume, workouts, each exercise's best set and estimated 1RM, and records.",
			Parameters: ai.Object("the window", map[string]*ai.Schema{
				"range": ai.Enum("the window", timerange.KeyWeek, timerange.KeyMonth, timerange.KeyQuarter, timerange.KeyYear),
			}),
		},
		ReadOnly:   true,
		Idempotent: true,
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}
			user, err := userSvc.ByID(ctx, userID)
			if err != nil {
				return "", err
			}
			key := in.Range
			if key == "" {
				key = timerange.KeyMonth
			}
			st, err := svc.Stats(ctx, user, timerange.Parse(key, user.Location()))
			if err != nil {
				return "", err
			}
			if len(st.Sets) == 0 {
				return "No sets logged in that window.", nil
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%d workouts, %d sets, %.0f kg volume (window before: %.0f kg).",
				lift.Workouts(st.Sets), len(st.Sets), st.VolumeKg(), st.PriorVolumeKg)
			for _, e := range st.Exercises {
				fmt.Fprintf(&b, "\n%s: %d sets, best %.1f kg, est. 1RM %.1f kg, last %s.",
					e.Name, e.Sets, e.BestWeightKg, e.BestE1RM, e.LastOn.In(user.Location()).Format("Jan 2"))
			}
			for _, r := range st.Records {
				fmt.Fprintf(&b, "\nRecord %s: %s %.1f kg × %d (est. %.1f kg, was %.1f).",
					r.Set.LogDate.Format("Jan 2"), r.Set.ExerciseName, r.Set.WeightKg, r.Set.Reps, r.E1RM, r.Previous)
			}
			return b.String(), nil
		},
	}
}

func getTrainingContext(svc *lifts.Service, userSvc *users.Service) Capability {
	return Capability{
		Tool: ai.Tool{
			Name: "get_training_context",
			Description: "Read how ready each muscle is to train today, from their logged sets: which are still fatigued or recovering, " +
				"which have gone untrained long enough to lose strength, when they last lifted, and records from the last two weeks. " +
				"Use it before suggesting what to train.",
			Parameters: ai.Object("no arguments", map[string]*ai.Schema{}),
		},
		ReadOnly:   true,
		Idempotent: true,
		Invoke: func(ctx context.Context, userID uuid.UUID, _ json.RawMessage) (string, error) {
			user, err := userSvc.ByID(ctx, userID)
			if err != nil {
				return "", err
			}
			load, err := svc.Readiness(ctx, user)
			if err != nil {
				return "", err
			}
			if load.LastSession.IsZero() {
				return "No sets logged yet, so there is nothing to read readiness from.", nil
			}
			_, records, err := svc.Recent(ctx, user)
			if err != nil {
				return "", err
			}
			var b strings.Builder
			b.WriteString(lift.ReadinessSummary(load))
			for _, m := range load.Muscles {
				fmt.Fprintf(&b, "\n%s: %s, last trained %s, strength %.0f%%.",
					m.Muscle, m.State, m.LastTrained.In(user.Location()).Format("Jan 2"), m.Strength*100)
			}
			for _, r := range records {
				fmt.Fprintf(&b, "\nRecord %s: %s %.1f kg × %d (est. 1RM %.1f kg).",
					r.Set.LogDate.Format("Jan 2"), r.Set.ExerciseName, r.Set.WeightKg, r.Set.Reps, r.E1RM)
			}
			return b.String(), nil
		},
	}
}

// clockToday reads "HH:MM" as a time today in loc, nil for empty. A time
// later than now is taken as yesterday, since a fast is started, not planned.
func clockToday(hhmm string, loc *time.Location, now time.Time) (*time.Time, error) {
	hhmm = strings.TrimSpace(hhmm)
	if hhmm == "" {
		return nil, nil
	}
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		return nil, apperr.Wrap(apperr.ErrValidation, "started_at must be HH:MM")
	}
	local := now.In(loc)
	at := time.Date(local.Year(), local.Month(), local.Day(), t.Hour(), t.Minute(), 0, 0, loc)
	if at.After(now) {
		at = at.AddDate(0, 0, -1)
	}
	return &at, nil
}
