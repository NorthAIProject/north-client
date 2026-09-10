package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/NorthAIProject/north-client/internal/activity"
	"github.com/NorthAIProject/north-client/internal/ai"
	apperr "github.com/NorthAIProject/north-client/internal/shared/errors"
	"github.com/NorthAIProject/north-client/internal/users"
)

// logActivity records a finished session by conversation: "ran 5k in 28
// minutes this morning."
//
// It joins the day's logs in logging.go for the same reason they exist: the
// one place someone is already typing was the one place a run could not be
// recorded. The tool takes the words the person used and resolves them with
// activity.Match, so the coach and the web form agree about what "a jog"
// means; when the words are ambiguous the error names the choices and the
// model asks or picks in one turn.
//
// Not ReadOnly, so it shows an approval card, and not Idempotent: two
// identical runs on one day are two runs.
func logActivity(svc *activity.Service, userSvc *users.Service) Capability {
	type args struct {
		Activity   string  `json:"activity"`
		Minutes    int     `json:"minutes"`
		DistanceKm float64 `json:"distance_km"`
		StartedAt  string  `json:"started_at"`
	}

	return Capability{
		Tool: ai.Tool{
			Name: "log_activity",
			Description: "Record a workout the user has already finished: a run, a walk, a ride, a swim, " +
				"a gym session. Give the activity in their own words and the duration in minutes. " +
				"Add the distance when they said one, so a run is stored with its pace. " +
				"Leave started_at empty when it just finished; otherwise give the local date and time " +
				"they started, such as '2026-09-10 07:30'. Convert miles to kilometres yourself. " +
				"Do not use this for a session still in progress.",
			Parameters: ai.Object("the finished session", map[string]*ai.Schema{
				"activity":    ai.String("what they did, in their words: 'run', 'brisk walk', 'swim', 'strength training'"),
				"minutes":     ai.Integer("how long it lasted, in minutes"),
				"distance_km": ai.Number("how far they went, in kilometres; 0 if they did not say or it does not apply"),
				"started_at":  ai.String("when it started, as 'YYYY-MM-DD HH:MM' in the user's local time; empty if it just finished"),
			}, "activity", "minutes", "distance_km", "started_at"),
		},
		Idempotent: false,
		Invoke: func(ctx context.Context, userID uuid.UUID, raw json.RawMessage) (string, error) {
			in, err := Decode[args](raw)
			if err != nil {
				return "", err
			}
			if in.Minutes <= 0 {
				return "", apperr.Wrap(apperr.ErrValidation, "say how many minutes the session lasted")
			}

			// The whole record for the timezone: a start time given as a
			// wall-clock reading means nothing without one.
			user, err := userSvc.ByID(ctx, userID)
			if err != nil {
				return "", err
			}

			startedAt, err := parseLocalTime(in.StartedAt, user.Location())
			if err != nil {
				return "", err
			}

			duration := time.Duration(in.Minutes) * time.Minute
			met, err := matchActivity(in.Activity, in.DistanceKm, duration)
			if err != nil {
				return "", err
			}

			session, err := svc.Log(ctx, userID, activity.LogInput{
				ActivityCode: met.Code,
				StartedAt:    startedAt,
				Duration:     duration,
				DistanceM:    in.DistanceKm * 1000,
			})
			if err != nil {
				return "", err
			}

			return describeLogged(session, met), nil
		},
	}
}

// matchActivity resolves the person's words, using the pace when there is
// one: someone who ran 5 km in 30 minutes has already said which running
// entry they mean.
func matchActivity(name string, distanceKm float64, duration time.Duration) (activity.MET, error) {
	if strings.TrimSpace(name) == "" {
		return activity.MET{}, apperr.Wrap(apperr.ErrValidation, "say what activity to log")
	}

	var speedKmh float64
	if distanceKm > 0 && duration > 0 {
		speedKmh = distanceKm / duration.Hours()
	}

	met, candidates := activity.Match(name, speedKmh)
	switch {
	case met.Code != "":
		return met, nil
	case len(candidates) > 0:
		return activity.MET{}, apperr.Wrap(apperr.ErrValidation,
			"%q could mean any of %s; say which, or give the distance so the pace decides", name, metNames(candidates))
	default:
		return activity.MET{}, apperr.Wrap(apperr.ErrNotFound,
			"no activity called %q; try a plainer word such as 'run', 'walk', 'cycling' or 'yoga'", name)
	}
}

func metNames(list []activity.MET) string {
	out := make([]string, 0, len(list))
	for _, m := range list {
		out = append(out, strconv.Quote(m.Name))
	}
	return strings.Join(out, ", ")
}

// parseLocalTime reads the start the model gives, in the person's zone. Empty
// is "just finished", and the service places the session to end now.
func parseLocalTime(raw string, loc *time.Location) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}

	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04", "2006-01-02T15:04"} {
		if t, err := time.ParseInLocation(layout, raw, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, apperr.Wrap(apperr.ErrValidation,
		"started_at %q is not a time; use 'YYYY-MM-DD HH:MM' or leave it empty", raw)
}

// describeLogged is the confirmation the model reads back: what was stored,
// with the pace when there is one, because that is the number a runner
// wants to hear repeated.
func describeLogged(s activity.Session, met activity.MET) string {
	minutes := int(s.Elapsed(*s.EndedAt).Round(time.Minute) / time.Minute)
	out := fmt.Sprintf("Logged %s, %d min", met.Name, minutes)

	if s.DistanceM != nil && *s.DistanceM > 0 {
		out += fmt.Sprintf(", %.2f km", *s.DistanceM/1000)
		if pace, ok := s.Pace(); ok {
			pace = pace.Round(time.Second)
			out += fmt.Sprintf(" at %d:%02d /km", int(pace/time.Minute), int(pace%time.Minute/time.Second))
		}
	}
	if s.CaloriesBurned != nil {
		out += fmt.Sprintf(", ~%.0f kcal", *s.CaloriesBurned)
	}
	return out + "."
}
